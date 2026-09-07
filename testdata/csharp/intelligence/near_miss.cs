class Process { public static void Start(string a, string b) {} }
class File { public static string ReadAllText(string value) => value; }
class HttpClient { public void GetAsync(string value) {} }
class Handler { void Run(string value) { Process.Start("cmd.exe", value); File.ReadAllText(value); new HttpClient().GetAsync(value); } }
