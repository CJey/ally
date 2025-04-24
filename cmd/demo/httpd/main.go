package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/cjey/ally"
)

var (
	Commit  = "dccd4b5f1c73e33543ceecf5224ebe00a9766137"
	Version = "1.0.3"

	Tasks int64
)

func main() {
	ally.Version, ally.Commit = Version, Commit
	ally.DynamicHook = func() (tasks uint64, desc string) {
		return uint64(Tasks), ""
	}

	lsn, err := ally.Listen("tcp", "127.0.0.1:8080")
	if err != nil {
		panic(err)
	}
	srv := http.Server{
		Handler:     http.HandlerFunc(Handler),
		IdleTimeout: 60 * time.Second,
	}
	go func() {
		srv.Serve(lsn)
	}()

	done := ally.NewEvent()
	ally.ExitHook = func() {
		defer done.Emit()
		fmt.Printf("%s[%d]: calling ExitHook\n", ally.Appname, ally.ID)
		srv.Shutdown(context.Background())
	}
	ally.Ready()

	fmt.Printf("%s[%d]: Runing at tcp://%s\n", ally.Appname, ally.ID, lsn.Addr().String())
	<-done.Yes
}

func Handler(resp http.ResponseWriter, req *http.Request) {
	atomic.AddInt64(&Tasks, 1)
	defer atomic.AddInt64(&Tasks, -1)

	var session = uuid.NewString()
	var pre = func() string {
		return fmt.Sprintf("%s %s[%d] %s",
			time.Now().Format("2006-01-02 15:04:05"),
			ally.Appname, ally.ID, session)
	}

	var buf = bytes.NewBuffer(nil)
	fmt.Printf("%s Request %s from %s\n", pre(), req.URL.Path, req.RemoteAddr)
	defer func() {
		fmt.Printf("%s Responsed to %s\n", pre(), req.RemoteAddr)
	}()

	buf.WriteString(fmt.Sprintf("%s sleep 100ms first\n", pre()))
	time.Sleep(100 * time.Millisecond)

	switch req.URL.Path {
	case "/reload":
		if err := ally.ReloadApp(); err != nil {
			buf.WriteString(fmt.Sprintf("%s ReloadApp call failed, %s\n", pre(), err))
		} else {
			buf.WriteString(fmt.Sprintf("%s reload ok\n", pre()))
		}
	case "/app":
		info := ally.GetAppInfo()
		output, _ := json.MarshalIndent(info, "", "   ")
		buf.WriteString(fmt.Sprintf("%s\n", pre()))
		buf.Write(output)
		buf.WriteString("\n")
	case "/instance":
		info := ally.GetInstanceInfo()
		output, _ := json.MarshalIndent(info, "", "   ")
		buf.WriteString(fmt.Sprintf("%s\n", pre()))
		buf.Write(output)
		buf.WriteString("\n")
	default:
		buf.WriteString("\n")
		buf.WriteString("/app       # Show this app info\n")
		buf.WriteString("/instance  # Show current instance info\n")
		buf.WriteString("/reload    # Reload this app\n")
	}
	resp.Write(buf.Bytes())
	fmt.Printf("%s", buf.Bytes())
}
