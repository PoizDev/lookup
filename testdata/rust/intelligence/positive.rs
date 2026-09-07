use axum::extract::Query;
use sqlx;
use reqwest;
use tokio;
use std::{fs, process::Command, thread, time::Duration};
async fn handler(Query(params): Query<Params>) {
    std::thread::sleep(Duration::from_secs(1));
    let sql = format!("SELECT * FROM users WHERE name = {}", params.name);
    sqlx::query(&sql).await;
    std::process::Command::new("sh").arg("-c").arg(&params.command).status();
    std::fs::read_to_string(&params.path);
    reqwest::get(&params.url).await;
    reqwest::Client::builder().danger_accept_invalid_certs(true);
}
