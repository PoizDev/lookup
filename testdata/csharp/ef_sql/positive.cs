using Microsoft.EntityFrameworkCore;
class Repo { void Run(DbContext db, string table) { var sql = "SELECT * FROM " + table; db.Database.ExecuteSqlRaw(sql); } }
