use std::collections::hash_map::DefaultHasher;
use std::env;
use std::hash::{Hash, Hasher};

use tokio::io::{AsyncBufReadExt, AsyncWriteExt, BufReader};
use tokio::net::{TcpListener, TcpStream};
use tokio::sync::broadcast;

/// Enum representing different events that occur during the servers runtime.
#[derive(Clone, Debug)]
enum Event {
    /// Broadcast message containing the author's nick and message content
    PublicMessage(String, String),
    /// Private message between two clients containing the author's and recipient's
    /// nicks and message content
    PrivateMessage(String, String, String),
    /// Event for an established connection with a user login
    Login(String),
    /// Unknown event as an error state
    Unknown,
}

impl Event {
    fn parse(s: &str, author: &String) -> Event {
        let split: Vec<&str> = s.split(" ").collect();
        match split[0] {
            "Login" => Event::Login(split[1].strip_suffix("\n").unwrap().to_string()),
            "Pubmsg" => Event::PublicMessage(author.clone(), split[1..].join(" ")),
            "Privmsg" => {
                Event::PrivateMessage(author.clone(), split[1].to_string(), split[2..].join(" "))
            }
            _ => Event::Unknown,
        }
    }
}

/// Handler function for a single connection that reads
/// all incoming messages and sends broadcasted messages
/// from it's own and other connections.
async fn handle_connection(
    sender: broadcast::Sender<Event>,
    mut receiver: broadcast::Receiver<Event>,
    mut socket: TcpStream,
) {
    let addr = socket
        .peer_addr()
        .unwrap_or_else(|_x| "127.0.0.1:0".parse().unwrap());

    let addr = addr.ip().to_string();

    let mut s = DefaultHasher::new();
    addr.hash(&mut s);
    let alt_nick = s.finish().to_string();

    println!("Client connected with ip {addr}");

    let (reader, mut writer) = socket.split();
    let mut reader = BufReader::new(reader);
    let mut buffer = String::new();

    let mut nick: Option<String> = None;

    loop {
        tokio::select! {
            read_result = reader.read_line(&mut buffer) => {
                match read_result {
                    Ok(bytes_read) => {
                        if bytes_read == 0 {
                            break;
                        }
                        let event = Event::parse(&buffer, nick.as_ref().unwrap_or(&alt_nick));
                        if nick.is_none() {
                            match &event {
                                Event::Login(new_nick) => {
                                    nick = Some(new_nick.clone())
                                }
                                _ => {
                                }
                            }
                        }
                        let sent = sender.send(event);
                        if let Err(e) = sent {
                            println!("Error broadcasting event: {}", e);
                        }
                        buffer.clear();
                    },
                    Err(e) => {
                        println!("Error occured: {}", e);
                    }
                }
            }

            event_result = receiver.recv() => {
                match event_result {
                    Ok(event) => {
                        match event {
                            Event::PublicMessage(author, msg) => {
                                writer.write_all(format!("{}: {}", &author, &msg).as_bytes()).await.unwrap();
                            }
                            Event::PrivateMessage(author, recipient, msg) => {
                                if let Some(nick) = &nick {
                                    if recipient == *nick {
                                        writer.write_all(format!("P {}: {}", author, msg).as_bytes()).await.unwrap();
                                    }
                                }
                            }
                            _ => {}
                        }
                    },
                    Err(e) => {
                        println!("Error receiving message: {}", e);
                    }
                }
            }
        }
    }
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let args: Vec<String> = env::args().collect();
    let addr = if args.len() < 2 {
        "127.0.0.1:6002"
    } else {
        &args[1]
    };

    let listener = TcpListener::bind(addr).await?;

    println!("Server started");

    let (tx, _) = broadcast::channel(10);

    loop {
        let (socket, _) = listener.accept().await?;

        let publisher = tx.clone();
        let consumer = tx.subscribe();
        tokio::spawn(async move {
            handle_connection(publisher, consumer, socket).await;
        });
    }
}
