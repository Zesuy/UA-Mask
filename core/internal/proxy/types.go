package proxy

import (
	"net"

	"UAmask/internal/model"
)

type Target = model.Target

type RequestProcessor interface {
	Process(dst net.Conn, src net.Conn, target Target)
}

type TrafficReporter interface {
	ReportHTTP(ip string, port int)
	ReportNonHTTP(ip string, port int)
}

type ConnMetrics interface {
	ConnectionOpened()
	ConnectionClosed()
}

type RequestMetrics interface {
	RecordHTTPRequest()
	RecordModifiedRequest()
	RecordCacheHitModified()
	RecordCacheHitPassthrough()
}
