package resources

import (
	"net/http"
	"os"
)

func Closed() {
	response, _ := http.Get("https://example.invalid")
	defer response.Body.Close()
	file, _ := os.Open("data.txt")
	defer file.Close()
}

func Escaped() (*os.File, error) { return os.Open("data.txt") }
