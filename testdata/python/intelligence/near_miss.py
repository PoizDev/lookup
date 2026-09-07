class Request:
    args = {"value": "literal"}

class Cursor:
    def execute(self, value):
        return value

def run(value):
    request = Request()
    Cursor().execute(value)
    resize(value)
    system(value)
