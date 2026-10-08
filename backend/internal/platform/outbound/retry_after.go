package outbound

import (
	"net/http"
	"strconv"
	"time"
)

func retryAfter(header http.Header, now time.Time) time.Duration {
	delay := time.Second
	if seconds, err := strconv.ParseInt(header.Get("Retry-After"), 10, 64); err == nil && seconds >= 0 {
		if seconds > 3600 {
			seconds = 3600
		}
		delay = time.Duration(seconds) * time.Second
	} else if date, err := http.ParseTime(header.Get("Retry-After")); err == nil {
		delay = date.Sub(now)
	}
	// Adapters normalize reviewed reset metadata into this internal header.
	if seconds, err := strconv.ParseInt(header.Get("X-Sama-Rate-Reset"), 10, 64); err == nil && seconds > now.Unix() && seconds-now.Unix() < 3600 {
		if reset := time.Unix(seconds, 0).Sub(now); reset > delay {
			delay = reset
		}
	}
	if delay < 0 {
		return 0
	}
	if delay > time.Hour {
		return time.Hour
	}
	return delay
}
