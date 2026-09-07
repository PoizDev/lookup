using Microsoft.AspNetCore.Http;
using Microsoft.EntityFrameworkCore;
using System.Diagnostics;
using System.IO;
using System.Net.Http;
using System.Threading.Tasks;
class Handler {
  async Task Run(HttpRequest request, DbContext db) {
    var query = request.Query["q"].ToString();
    var path = request.Query["path"].ToString();
    var command = request.Query["cmd"].ToString();
    var url = request.Query["url"].ToString();
    db.Database.ExecuteSqlRaw(query);
    Process.Start("cmd.exe", "/c " + command);
    File.ReadAllText(path);
    await new HttpClient().GetAsync(url);
    Task.Delay(1).Wait();
    var handler = new HttpClientHandler();
    handler.ServerCertificateCustomValidationCallback = (_, _, _, _) => true;
  }
}
