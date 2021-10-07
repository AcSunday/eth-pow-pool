package test

import (
	"github.com/etclabscore/core-pool/util/logger"
	"net/http"
	"testing"
)

func simpleHttpGet(url string) {
	logger.Warn("Trying to hit GET request for %s", url)
	resp, err := http.Get(url)
	if err != nil {
		logger.Error("Error fetching URL %s : Error = %s", url, err)
	} else {
		logger.Info("Success! statusCode = %s for URL %s", resp.Status, url)
		resp.Body.Close()
	}
}

func TestLogger(t *testing.T) {
	logger.InitTimeLogger("./run.log", "./run_err.log", 7, 10)
	defer logger.Sync()

	simpleHttpGet("www.sogo.com")
	simpleHttpGet("http://www.sogo.com")
	logger.Info("Success! statusCode = test")
}
