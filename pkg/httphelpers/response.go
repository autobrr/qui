package httphelpers

import (
	"io"
	"net/http"
)

// DrainAndClose consumes the remaining response body and closes it to allow connection reuse.
func DrainAndClose(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
}
