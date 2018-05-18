package selfFastHttp

import (
	"fmt"
	"sync/atomic"
)

const (
	StatusContinue           = 100 // RFC 7231, 6.2.1 客户端应当继续发送请求，这个临时响应是用来通知客户端它的部分请求已经被服务器接收，且仍未被拒绝。客户端应当继续发送请求的剩余部分，或者如果请求已经完成，忽略这个响应。服务器必须在请求完成后向客户端发送一个最终响应。
	StatusSwitchingProtocols = 101 // RFC 7231, 6.2.2 服务器已经理解了客户端的请求，并将通过Upgrade 消息头通知客户端采用不同的协议来完成这个请求。在发送完这个响应最后的空行后，服务器将会切换到在Upgrade 消息头中定义的那些协议。 　　只有在切换新的协议更有好处的时候才应该采取类似措施。例如，切换到新的HTTP 版本比旧版本更有优势，或者切换到一个实时且同步的协议以传送利用此类特性的资源。
	StatusProcessing         = 102 // RFC 2518, 10.1  由WebDAV（RFC 2518）扩展的状态码，代表处理将被继续执行。

	//2xx 请求成功处理
	StatusOK                   = 200 // RFC 7231, 6.3.1 请求已成功，请求所希望的响应头或数据体将随此响应返回。
	StatusCreated              = 201 // RFC 7231, 6.3.2 请求成功，并相应创建新资源，且其 URI 已经随Location 头信息返回
	StatusAccepted             = 202 // RFC 7231, 6.3.3 请求已接受，尚未处理(允许服务器接受其他过程的请求，而不必让客户端一直保持与服务器的连接直到批处理操作全部完成)
	StatusNonAuthoritativeInfo = 203 // RFC 7231, 6.3.4 非授权的信息,返回内容为第三方源
	StatusNoContent            = 204 // RFC 7231, 6.3.5 未返回内容
	StatusResetContent         = 205 // RFC 7231, 6.3.6 未返回内容，要求请求方重置文档视图
	StatusPartialContent       = 206 // RFC 7233, 4.1	断点续传等使用，返回指定片断的资源
	StatusMultiStatus          = 207 // RFC 4918, 11.1  WebDAV(RFC 2518)扩展的状态码，代表之后的消息体是一个XML消息，并可能依照之前子请求数量的不同，包含一系列独立的响应代码
	StatusAlreadyReported      = 208 // RFC 5842, 7.1
	StatusIMUsed               = 209 // RFC 3229, 10.4.1

	//3xx （请求被重定向）表示要完成请求，需要进一步操作。 通常，这些状态代码用来重定向。
	StatusMultipleChoices   = 300 // RFC 7231, 6.4.1 被请求的资源有一系列可供选择的回馈信息，每个都有自己特定的地址和浏览器驱动的商议信息。用户或浏览器能够自行选择一个首选的地址进行重定向。
	StatusMovedPermanently  = 301 // RFC 7231, 6.4.2 被请求的资源已永久移动到新位置，并且将来任何对此资源的引用都应该使用本响应返回的若干个 URI 之一
	StatusFound             = 302 // RFC 7231, 6.4.3 请求的资源现在临时从不同的 URI 响应请求。由于这样的重定向是临时的，客户端应当继续向原有地址发送以后的请求。只有在Cache-Control或Expires中进行了指定的情况下，这个响应才是可缓存的。
	StatusSeeOther          = 303 // RFC 7231, 6.4.4 对应当前请求的响应可以在另一个 URI 上被找到，而且客户端应当采用 GET 的方式访问那个资源。
	StatusNotModified       = 304 // RFC 7232, 4.1   如果客户端发送了一个带条件的 GET 请求且该请求已被允许，而文档的内容（自上次访问以来或者根据请求的条件）并没有改变，则服务器应当返回这个状态码。304响应禁止包含消息体，因此始终以消息头后的第一个空行结尾。
	StatusUseProxy          = 305 // RFC 7231, 6.4.5 被请求的资源必须通过指定的代理才能被访问。Location 域中将给出指定的代理所在的 URI 信息，接收者需要重复发送一个单独的请求，通过这个代理才能访问相应资源。只有原始服务器才能建立305响应。
	_                       = 306 // RFC 7231, 6.4.6(Unused)
	StatusTemporaryRedirect = 307 // RFC 7231, 6.4.7 请求的资源现在临时从不同的URI 响应请求。请求的资源现在临时从不同的URI 响应请求。
	StatusPermanentRedirect = 308 // RFC 7538, 3     请求的资源已永久移动到新位置。

	//4xx （请求错误）这些状态代码表示请求可能出错，妨碍了服务器的处理。
	StatusBadRequest                   = 400 // RFC 7231, 6.5.1 （错误请求）服务器不理解请求的参数。
	StatusUnauthorized                 = 401 // RFC 7235, 3.1   （未授权） 请求 要求身份验证。对于需要登录的网页，服务器可能返回此响应。
	StatusPaymentRequired              = 402 // RFC 7231, 6.5.2 该状态码是为了将来可能的需求而预留的。
	StatusForbidden                    = 403 // RFC 7231, 6.5.3 （禁止） 服务器已经理解请求，但是拒绝执行它。
	StatusNotFound                     = 404 // RFC 7231, 6.5.4 请求失败，请求所希望得到的资源未被在服务器上发现。
	StatusMethodNotAllowed             = 405 // RFC 7231, 6.5.5 请求行中指定的请求方法不能被用于请求相应的资源。该响应必须返回一个Allow 头信息用以表示出当前资源能够接受的请求方法的列表。鉴于 PUT，DELETE 方法会对服务器上的资源进行写操作，因而绝大部分的网页服务器都不支持或者在默认配置下不允许上述请求方法，对于此类请求均会返回405错误。
	StatusNotAcceptable                = 406 // RFC 7231, 6.5.6 请求的资源的内容特性无法满足请求头中的条件，因而无法生成响应实体。
	StatusProxyAuthRequired            = 407 // RFC 7235, 3.2   与401响应类似，只不过客户端必须在代理服务器上进行身份验证。
	StatusRequestTimeout               = 408 // RFC 7231, 6.5.7 请求超时。客户端没有在服务器预备等待的时间内完成一个请求的发送。客户端可以随时再次提交这一请求而无需进行任何更改。
	StatusConflict                     = 409 // RFC 7231, 6.5.8 由于和被请求的资源的当前状态之间存在冲突，请求无法完成。这个代码只允许用在这样的情况下才能被使用：用户被认为能够解决冲突，并且会重新提交新的请求。该响应应当包含足够的信息以便用户发现冲突的源头。 冲突通常发生于对 PUT 请求的处理中。例如，在采用版本检查的环境下，某次 PUT 提交的对特定资源的修改请求所附带的版本信息与之前的某个（第三方）请求向冲突，那么此时服务器就应该返回一个409错误，告知用户请求无法完成。此时，响应实体中很可能会包含两个冲突版本之间的差异比较，以便用户重新提交归并以后的新版本。
	StatusGone                         = 410 // RFC 7231, 6.5.9 被请求的资源在服务器上已经不再可用，而且没有任何已知的转发地址。这样的状况应当被认为是永久性的。
	StatusLengthRequired               = 411 // RFC 7231, 6.5.10 服务器拒绝在没有定义 Content-Length 头的情况下接受请求。
	StatusPreconditionFailed           = 412 // RFC 7232, 4.2    服务器在验证在请求的头字段中给出先决条件时，没能满足其中的一个或多个。
	StatusRequestEntityTooLarge        = 413 // RFC 7231, 6.5.11 服务器拒绝处理当前请求，因为该请求提交的实体数据大小超过了服务器愿意或者能够处理的范围。
	StatusRequestURITooLong            = 414 // RFC 7231, 6.5.12 请求的URI长度超过了服务器能够解释的长度，因此服务器拒绝对该请求提供服务。
	StatusUnsupportedMediaType         = 415 // RFC 7231, 6.5.13 对于当前请求的方法和所请求的资源，请求中提交的实体并不是服务器中所支持的格式，因此请求被拒绝。
	StatusRequestedRangeNotSatisfiable = 416 // RFC 7232, 4.4    如果请求中包含了Range请求头，并且Range中指定的任何数据范围都与当前资源的可用范围不重合，同时请求中又没有定义If-Range请求头，那么服务器就应当返回416状态码。
	StatusExpectationFailed            = 417 // RFC 7231, 6.5.14 在请求头Expect中指定的预期内容无法被服务器满足，或者这个服务器是一个代理服务器，它有明显的证据证明在当前路由的下一个节点上，Expect的内容无法被满足。
	StatusTeapot                       = 418 // RFC 7168, 2.3.3  愚人节玩笑
	StatusIPTooManyConnection          = 421 // 	从当前客户端所在的IP地址到服务器的连接数超过了服务器许可的最大范围。
	StatusUnprocessableEntity          = 422 // RFC 4918, 11.2 请求格式正确，但是由于含有语义错误，无法响应。
	StatusLocked                       = 423 // RFC 4918, 11.3 当前资源被锁定。（RFC 4918 WebDAV）
	StatusFailedDependency             = 424 // RFC 4918, 11.4 由于之前的某个请求发生的错误，导致当前请求失败，例如 PROPPATCH。
	StatusUpgradeRequired              = 426 // RFC 7231, 6.5.15 客户端应当切换到TLS/1.0。（RFC 2817）
	StatusPreconditionRequired         = 428 // RFC 6585, 3    要求先决条件
	StatusTooManyRequests              = 429 // RFC 6585, 4    太多请求
	StatusRequestHeaderFieldsTooLarge  = 431 // RFC 6585, 5    某些情况下，客户端发送HTTP请求头会变得很大，那么服务器可发送431来指明该问题。
	StatusUnavailableForLegalReasons   = 451 // RFC 7725, 3    因法律原因而被官方审查,由于法律原因产生的后果而被官方拒绝访问

	//5xx （服务器错误）这些状态代码表示服务器在尝试处理请求时发生内部错误。 这些错误可能是服务器本身的错误，而不是请求出错。
	StatusInternalServerError           = 500 // RFC 7231, 6.6.1 服务器遇到了一个未曾预料的状况，导致了它无法完成对请求的处理。
	StatusNotImplemented                = 501 // RFC 7231, 6.6.2 服务器不支持当前请求所需要的某个功能。
	StatusBadGateway                    = 502 // RFC 7231, 6.6.3 作为网关或者代理工作的服务器尝试执行请求时，从上游服务器接收到无效的响应。
	StatusServiceUnavailable            = 503 // RFC 7231, 6.6.4 由于临时的服务器维护或者过载，服务器当前无法处理请求。
	StatusGatewayTimeout                = 504 // RFC 7231, 6.6.5 作为网关或者代理工作的服务器尝试执行请求时，未能及时从上游服务器（URI标识出的服务器，例如HTTP、FTP、LDAP）或者辅助服务器（例如DNS）收到响应。
	StatusHTTPVersionNotSupported       = 505 // RFC 7231, 6.6.6 服务器不支持，或者拒绝支持在请求中使用的 HTTP 版本。
	StatusVariantAlsoNegotiates         = 506 // RFC 2295, 8.1   由《透明内容协商协议》（RFC 2295）扩展，是因为服务器没有正确配置：被请求的协商变元资源被配置为在透明内容协商中使用自己，因此在一个协商处理中不是一个合适的重点。
	StatusInsufficientStorage           = 507 // RFC 4918, 11.5  服务器无法存储完成请求所必须的内容。这个状况被认为是临时的。WebDAV (RFC 4918)
	StatusLoopDetected                  = 508 // RFC 5842, 7.2   请求处理死循环
	StatusNotExtended                   = 510 // RFC 2774, 7     获取资源所需要的策略并没有没满足。（RFC 2774）
	StatusNetworkAuthenticationRequired = 511 // RFC 6585, 6     要求网络认证.大量的公用 Wifi 服务要求你必须接受一些协议或者必须登录后才能使用，这是通过拦截HTTP流量实现的。当用户试图访问网络返回一个重定向和登录，这很讨厌，但是实际情况就是这样的。
)

var (
	statusLines atomic.Value

	statusMessages = map[int]string{
		StatusContinue:           "Continue",
		StatusSwitchingProtocols: "Switching Protocols",
		StatusProcessing:         "Processing",

		StatusOK:                   "OK",
		StatusCreated:              "Created",
		StatusAccepted:             "Accepted",
		StatusNonAuthoritativeInfo: "Non-Authoritative Information",
		StatusNoContent:            "No Content",
		StatusResetContent:         "Reset Content",
		StatusPartialContent:       "Partial Content",
		StatusMultiStatus:          "Multi-Status",
		StatusAlreadyReported:      "Already Reported",
		StatusIMUsed:               "IM Used",

		StatusMultipleChoices:   "Multiple Choices",
		StatusMovedPermanently:  "Moved Permanently",
		StatusFound:             "Found",
		StatusSeeOther:          "See Other",
		StatusNotModified:       "Not Modified",
		StatusUseProxy:          "Use Proxy",
		StatusTemporaryRedirect: "Temporary Redirect",
		StatusPermanentRedirect: "Permanent Redirect",

		StatusBadRequest:                   "Bad Request",
		StatusUnauthorized:                 "Unauthorized",
		StatusPaymentRequired:              "Payment Required",
		StatusForbidden:                    "Forbidden",
		StatusNotFound:                     "Not Found",
		StatusMethodNotAllowed:             "Method Not Allowed",
		StatusNotAcceptable:                "Not Acceptable",
		StatusProxyAuthRequired:            "Proxy Authentication Required",
		StatusRequestTimeout:               "Request Timeout",
		StatusConflict:                     "Conflict",
		StatusGone:                         "Gone",
		StatusLengthRequired:               "Length Required",
		StatusPreconditionFailed:           "Precondition Failed",
		StatusRequestEntityTooLarge:        "Request Entity Too Large",
		StatusRequestURITooLong:            "Request URI Too Long",
		StatusUnsupportedMediaType:         "Unsupported Media Type",
		StatusRequestedRangeNotSatisfiable: "Requested Range Not Satisfiable",
		StatusExpectationFailed:            "Expectation Failed",
		StatusTeapot:                       "I'm a teapot",
		StatusUnprocessableEntity:          "Unprocessable Entity",
		StatusLocked:                       "Locked",
		StatusFailedDependency:             "Failed Dependency",
		StatusUpgradeRequired:              "Upgrade Required",
		StatusPreconditionRequired:         "Precondition Required",
		StatusTooManyRequests:              "Too Many Requests",
		StatusRequestHeaderFieldsTooLarge:  "Request Header Fields Too Large",
		StatusUnavailableForLegalReasons:   "Unavailable For Legal Reasons",

		StatusInternalServerError:           "Internal Server Error",
		StatusNotImplemented:                "Not Implemented",
		StatusBadGateway:                    "Bad Gateway",
		StatusServiceUnavailable:            "Service Unavailable",
		StatusGatewayTimeout:                "Gateway Timeout",
		StatusHTTPVersionNotSupported:       "HTTP Version Not Supported",
		StatusVariantAlsoNegotiates:         "Variant Also Negotiates",
		StatusInsufficientStorage:           "Insufficient Storage",
		StatusLoopDetected:                  "Loop Detected",
		StatusNotExtended:                   "Not Extended",
		StatusNetworkAuthenticationRequired: "Network Authentication Required",
	}
)

//return HTTP status message for the given status code
func StatusMessage(statusCode int) string {
	s := statusMessages[statusCode]
	if s == "" {
		s = "Unkonwn Status Code"
	}
	return s
}

func init() {
	m := make(map[int][]byte, len(statusMessages))
	for k, v := range statusMessages {
		m[k] = []byte(fmt.Sprintf("HTTP/1.1 %d %s \r\n", k, v))
	}
	statusLines.Store(m)
}

func statusLine(statusCode int) []byte {
	m := statusLines.Load().(map[int][]byte) //m不逃逸
	h := m[statusCode]
	if h != nil {
		return h
	}

	statusText := StatusMessage(statusCode)

	h = []byte(fmt.Sprintf("HTTP/1.1 %d %s \r\n", statusCode, statusText))
	newM := make(map[int][]byte, len(m)+1) //escapes to heap
	for k, v := range m {
		newM[k] = v
	}
	newM[statusCode] = h
	statusLines.Store(newM)
	return h
}
