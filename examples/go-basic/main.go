// Command go-basic is a small HTTP service instrumented with the izci Go
// SDK: panics and 5xx responses are reported by the izcihttp middleware,
// error logs by the izcislog handler, and every event carries the customer,
// the user and the request.
//
//	IZCI_ENDPOINT=http://localhost:5300 IZCI_KEY=izk_… go run ./examples/go-basic -demo
//
// With -demo the program sends a few requests to itself and exits;
// otherwise it listens on :8081.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	izci "github.com/mcihad/kentos-izci/sdk/go"
	"github.com/mcihad/kentos-izci/sdk/go/izcihttp"
	"github.com/mcihad/kentos-izci/sdk/go/izcislog"
)

var errOutOfStock = errors.New("out of stock")

type order struct {
	ID    string
	Owner *struct{ Name string }
}

func findOrder(id string) (*order, error) {
	if id == "404" {
		return nil, fmt.Errorf("find order %s: %w", id, errOutOfStock)
	}
	return &order{ID: id}, nil
}

func main() {
	demo := flag.Bool("demo", false, "send demo requests to itself and exit")
	flag.Parse()

	if err := izci.Init(izci.Options{
		Endpoint:     getenv("IZCI_ENDPOINT", "http://localhost:5300"),
		Key:          os.Getenv("IZCI_KEY"),
		Environment:  "production",
		Release:      "go-basic@1.0.0",
		CustomerID:   "sivas-belediyesi",
		CustomerName: "Sivas Belediyesi",
		Tags:         map[string]string{"service": "orders"},
	}); err != nil {
		log.Fatal(err)
	}
	defer izci.Close(3 * time.Second)

	logger := slog.New(izcislog.New(slog.NewTextHandler(os.Stderr, nil), izcislog.Options{}))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		izci.FromContext(r.Context()).SetUser(izci.User{ID: "42", Email: "ayse@example.com", IPAddress: "85.96.10.20"})
		o, err := findOrder(r.PathValue("id"))
		if err != nil {
			logger.ErrorContext(r.Context(), "order lookup failed", "err", err, "order_id", r.PathValue("id"))
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		// Bug: orders without an owner crash the handler.
		fmt.Fprintf(w, "order %s owned by %s\n", o.ID, o.Owner.Name)
	})

	handler := izcihttp.New(izcihttp.Options{}).Handle(mux)
	if !*demo {
		log.Println("listening on :8081")
		log.Fatal(http.ListenAndServe(":8081", handler))
	}

	srv := httptest.NewServer(handler)
	defer srv.Close()
	for _, path := range []string{"/orders/7", "/orders/8", "/orders/404"} {
		res, err := http.Get(srv.URL + path)
		if err != nil {
			log.Fatal(err)
		}
		res.Body.Close()
		fmt.Printf("GET %s → %d\n", path, res.StatusCode)
	}
	if !izci.Flush(5 * time.Second) {
		log.Fatal("events were not delivered in time")
	}
	fmt.Println("events delivered; dropped:", izci.CurrentClient().Dropped())
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
