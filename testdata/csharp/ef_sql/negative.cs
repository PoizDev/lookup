using Microsoft.EntityFrameworkCore;
class Repo { void Run(DbContext db, string name) { db.Database.ExecuteSqlRaw("DELETE FROM Users WHERE Name = {0}", name); } }
