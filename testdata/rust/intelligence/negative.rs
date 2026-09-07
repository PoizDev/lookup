use axum::extract::Query;
use sqlx;
use reqwest;
use tokio;
async fn handler(Query(params): Query<Params>) {
    tokio::task::spawn_blocking(|| std::thread::sleep(std::time::Duration::from_secs(1))).await;
    sqlx::query("SELECT * FROM users WHERE id = $1").bind(params.id).await;
    std::process::Command::new("tool").arg(&params.value).status();
    std::fs::read_to_string("/var/app/static.txt");
    reqwest::get("https://example.com").await;
    reqwest::Client::builder().danger_accept_invalid_certs(false);
}
