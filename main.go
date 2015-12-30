package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime/debug"
	"time"

	"github.com/getsentry/raven-go"
	"github.com/go-zoo/bone"
	"github.com/rs/xhandler"
	"github.com/yvasiyarov/go-metrics"
	"github.com/yvasiyarov/gorelic"
	"golang.org/x/net/context"
)

func NewGorelic(license string, appname string, verbose bool) *gorelic.Agent {
	if license == "" {
		panic("Please specify NewRelic license")
	}

	agent := gorelic.NewAgent()
	agent.NewrelicLicense = license
	agent.HTTPTimer = metrics.NewTimer()
	agent.CollectHTTPStat = true
	agent.Verbose = verbose

	agent.NewrelicName = appname
	agent.Run()

	return agent
}

func createNewrelicMiddleware() func(next http.Handler) http.Handler {
	newrelicLicense := os.Getenv("NEWRELIC_LICENSE_KEY")
	log.Printf("%s", newrelicLicense)
	agent := NewGorelic(newrelicLicense, "Golang Test", true)
	mid := func(next http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			startTime := time.Now()
			next.ServeHTTP(w, r)
			agent.HTTPTimer.UpdateSince(startTime)
		}
		return http.HandlerFunc(fn)
	}
	return mid
}

func sentryMiddleware(next http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		raven.SetHttpContext(raven.NewHttp(r))
		defer func() {
			if err := recover(); err != nil {
				debug.PrintStack()
				errStr := fmt.Sprint(err)
				packet := raven.NewPacket(
					errStr,
					raven.NewException(errors.New(errStr), raven.NewStacktrace(2, 3, nil)),
					raven.NewHttp(r))
				raven.Capture(packet, nil)
				http.Error(
					w,
					http.StatusText(http.StatusInternalServerError),
					http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	}
	return http.HandlerFunc(fn)
}

func loggingMiddleware(next http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		t1 := time.Now()
		next.ServeHTTP(w, r)
		t2 := time.Now()
		log.Printf("[%s] %q %v\n", r.Method, r.URL.String(), t2.Sub(t1))
	}
	return http.HandlerFunc(fn)
}

func normalHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "normal")
}

func contextNormalHandler(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "normal with context")
}

func panicHandler(w http.ResponseWriter, r *http.Request) {
	err := errors.New("error for sentry: from panicHandler")
	panic(err)
	fmt.Fprintf(w, "panic")
}

func contextPanicHandler(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	err := errors.New("error for sentry: from contextPanicHandler")
	panic(err)
	fmt.Fprintf(w, "panic with context")
}

func main() {
	newrelicMiddleware := createNewrelicMiddleware()
	c := xhandler.Chain{}
	c.Use(sentryMiddleware)
	c.Use(newrelicMiddleware)
	c.Use(loggingMiddleware)
	c.UseC(xhandler.CloseHandler)
	c.UseC(xhandler.TimeoutHandler(2 * time.Second))

	mux := bone.New()
	mux.Get("/normal", c.HandlerF(normalHandler))
	mux.Get("/panic", c.HandlerF(panicHandler))
	mux.Get("/context/normal", c.HandlerFC(contextNormalHandler))
	mux.Get("/context/panic", c.HandlerFC(contextPanicHandler))

	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatal(err)
	}
}
