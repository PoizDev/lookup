mod reqwest { pub fn get(value: &str) {} }
mod sqlx { pub fn query(value: &str) {} }
fn run(value: &str) { reqwest::get(value); sqlx::query(value); sleep(value); }
