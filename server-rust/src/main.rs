use std::net::{IpAddr, Ipv4Addr, SocketAddr};

use tokio::io::{AsyncBufReadExt, AsyncWriteExt, BufReader};
use tokio::net::{TcpListener, TcpStream};
use tokio::sync::broadcast;

/// Semantic representation of a message received by the server
#[derive(Clone, Debug)]
struct Message(String, String);

impl Message {
    fn new(author: String, message: String) -> Message {
        Message(author, message)
    }
}

/// Handler function for a single connection that reads
/// all incoming messages and sends broadcasted messages
/// from it's own and other connections.
async fn handle_connection(
    sender: broadcast::Sender<Message>,
    mut receiver: broadcast::Receiver<Message>,
    mut socket: TcpStream,
) {
    // Address of the connecting client serves as the username for now
    // TODO: replace with login system or unique ids
    let addr = socket
        .peer_addr()
        .unwrap_or_else(|_x| SocketAddr::new(IpAddr::V4(Ipv4Addr::new(127, 0, 0, 1)), 6002));
    let addr = addr.ip().to_string();

    println!("Client connected with ip {addr}");

    let (reader, mut writer) = socket.split();
    let mut reader = BufReader::new(reader);
    let mut buffer = String::new();

    loop {
        tokio::select! {
            read_result = reader.read_line(&mut buffer) => {
                match read_result {
                    Ok(bytes_read) => {
                        if bytes_read == 0 {
                            continue;
                        }
                        let sent = sender.send(Message::new(addr.clone(), buffer.clone()));
                        if let Err(e) = sent {
                            println!("Error broadcasting message: {}", e);
                        }
                        buffer.clear();
                    },
                    Err(e) => {
                        println!("Error occured: {}", e);
                    }
                }
            }

            msg_result = receiver.recv() => {
                match msg_result {
                    Ok(message) => {
                        let Message(author, message) = message;
                        println!("{}: {}", author, message);
                        writer.write_all(format!("{}: {}", author, message).as_bytes()).await.unwrap();
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
    // for now simply bind to a specific port and ip
    // TODO: allow env args / cli args to set specific values
    let listener = TcpListener::bind("127.0.0.1:6002").await?;

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
