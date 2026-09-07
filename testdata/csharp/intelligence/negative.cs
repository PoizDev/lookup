using Microsoft.AspNetCore.Http;
using Microsoft.EntityFrameworkCore;
using System.Diagnostics;
using System.IO;
using System.Net.Http;
using System.Threading.Tasks;
class Handler {
  async Task Run(HttpRequest request, DbContext db) {
    var value = request.Query["value"].ToString();
    db.Database.ExecuteSqlRaw("DELETE FROM Users WHERE Id = {0}", value);
    Process.Start("tool.exe", value);
    File.ReadAllText("/var/app/static.txt");
    await new HttpClient().GetAsync("https://example.com");
    await Task.Delay(1);
    var handler = new HttpClientHandler();
    handler.ServerCertificateCustomValidationCallback = ValidateCertificate;
  }
}
