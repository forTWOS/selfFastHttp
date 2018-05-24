package selfFastHttp

import (
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/valyala/fasthttp"
)

var (
	addr     = flag.String("addr", ":8080", "TCP address to listen to")
	compress = flag.Bool("compress", false, "Whether to enable transparent response compression")
)

func init() {
	flag.Parse()

	h := requestHandler
	if *compress {
		h = fasthttp.CompressHandler(h)
	}

	if err := fasthttp.ListenAndServe(*addr, h); err != nil {
		log.Fatalf("Error in ListenAndServe: %s", err)
	}
}

func requestHandler(ctx *fasthttp.RequestCtx) {
	fmt.Fprintf(ctx, "Hello, world!\n\n")

	fmt.Fprintf(ctx, "Request method is %q\n", ctx.Method())
	fmt.Fprintf(ctx, "RequestURI is %q\n", ctx.RequestURI())
	fmt.Fprintf(ctx, "Requested path is %q\n", ctx.Path())
	fmt.Fprintf(ctx, "Host is %q\n", ctx.Host())
	fmt.Fprintf(ctx, "Query string is %q\n", ctx.QueryArgs())
	fmt.Fprintf(ctx, "User-Agent is %q\n", ctx.UserAgent())
	fmt.Fprintf(ctx, "Connection has been established at %s\n", ctx.ConnTime())
	fmt.Fprintf(ctx, "Request has been started at %s\n", ctx.Time())
	fmt.Fprintf(ctx, "Serial request number for the current connection is %d\n", ctx.ConnRequestNum())
	fmt.Fprintf(ctx, "Your ip is %q\n\n", ctx.RemoteIP())

	fmt.Fprintf(ctx, "Raw request is:\n---CUT---\n%s\n---CUT---", &ctx.Request)

	ctx.SetContentType("text/plain; charset=utf8")

	// Set arbitrary headers
	ctx.Response.Header.Set("X-My-Header", "my-header-value")

	// Set cookies
	var c fasthttp.Cookie
	c.SetKey("cookie-name")
	c.SetValue("cookie-value")
	ctx.Response.Header.SetCookie(&c)
}

var gCli = &fasthttp.Client{
	/*Dial: func(addr string) (net.Conn, error) {
		return ln.Dial()
	},*/
}

//http请求
func doTimeout(arg *fasthttp.Args, method string, requestURI string, cookies map[string]interface{}) ([]byte, int, error) {
	req := &fasthttp.Request{}
	switch method {
	case "GET":
		req.Header.SetMethod(method)
		// 拼接url
		requestURI = requestURI + "?" + arg.String()
	case "POST":
		req.Header.SetMethod(method)
		arg.WriteTo(req.BodyWriter())
	}
	if cookies != nil {
		for key, v := range cookies {
			req.Header.SetCookie(key, v.(string))
		}
	}
	req.SetRequestURI(requestURI)

	resp := &fasthttp.Response{}
	err := gCli.DoTimeout(req, resp, time.Second*30)

	return resp.Body(), resp.StatusCode(), err
}

func doJsonTimeout(method string, url, bodyjson string) ([]byte, int, error) {
	req := &fasthttp.Request{}
	resp := &fasthttp.Response{}

	switch method {
	case "GET":
		req.Header.SetMethod(method)
	case "POST":
		req.Header.SetMethod(method)
	}

	req.Header.SetContentType("application/json")
	req.SetBodyString(bodyjson)

	req.SetRequestURI(url)

	err := gCli.DoTimeout(req, resp, time.Second*30)
	return resp.Body(), resp.StatusCode(), err
}
