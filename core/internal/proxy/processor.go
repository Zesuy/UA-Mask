package proxy

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"

	"UAmask/internal/config"
	"UAmask/internal/rewrite"

	"github.com/sirupsen/logrus"
)

var httpMethods = []string{"GET", "POST", "HEAD", "PUT", "DELETE", "OPTIONS", "TRACE", "CONNECT", "PATCH"}

type Processor struct {
	rewriter       *rewrite.RequestRewriter
	reporter       TrafficReporter
	requestMetrics RequestMetrics
	readerPool     sync.Pool
	writerPool     sync.Pool
}

func NewProcessor(cfg config.ListenConfig, rewriter *rewrite.RequestRewriter, reporter TrafficReporter, requestMetrics RequestMetrics) *Processor {
	processor := &Processor{
		rewriter:       rewriter,
		reporter:       reporter,
		requestMetrics: requestMetrics,
	}

	processor.readerPool = sync.Pool{
		New: func() any {
			return bufio.NewReaderSize(nil, cfg.BufferSize)
		},
	}
	processor.writerPool = sync.Pool{
		New: func() any {
			return bufio.NewWriterSize(nil, cfg.BufferSize)
		},
	}

	return processor
}

func (p *Processor) Process(dst net.Conn, src net.Conn, target Target) {
	srcReader := p.readerPool.Get().(*bufio.Reader)
	srcReader.Reset(src)
	defer p.readerPool.Put(srcReader)

	dstWriter := p.writerPool.Get().(*bufio.Writer)
	dstWriter.Reset(dst)
	defer func() {
		if err := dstWriter.Flush(); err != nil {
			logrus.Debugf("[Processor] [%s] final flush error: %v", target.Address, err)
		}
		p.writerPool.Put(dstWriter)
	}()

	for {
		isHTTP, err := p.isHTTP(srcReader)
		if err != nil {
			p.handleDetectionError(dst, srcReader, dstWriter, target, err)
			return
		}

		if !isHTTP {
			logrus.Debugf("[Processor] [%s] non-HTTP traffic detected", target.Address)
			p.reporter.ReportNonHTTP(target.IP, target.Port)
			p.flushAndCopy(dst, srcReader, dstWriter, target)
			return
		}

		if !p.processRequest(dstWriter, srcReader, target) {
			return
		}
	}
}

func (p *Processor) isHTTP(reader *bufio.Reader) (bool, error) {
	buf, err := reader.Peek(7)
	if err != nil {
		if !strings.Contains(err.Error(), "EOF") {
			logrus.Debugf("[Processor] Peek error: %s", err.Error())
		}
		return false, err
	}

	hint := string(buf)
	for _, method := range httpMethods {
		if strings.HasPrefix(hint, method) {
			return true, nil
		}
	}
	return false, nil
}

func (p *Processor) handleDetectionError(dst net.Conn, srcReader *bufio.Reader, dstWriter *bufio.Writer, target Target, err error) {
	if err == io.EOF || strings.Contains(err.Error(), "use of closed network connection") {
		logrus.Debugf("[Processor] [%s] connection closed", target.Address)
	} else {
		logrus.Debugf("[Processor] [%s] HTTP detection error: %v", target.Address, err)
	}

	p.flushAndCopy(dst, srcReader, dstWriter, target)
}

func (p *Processor) flushAndCopy(dst net.Conn, srcReader *bufio.Reader, dstWriter *bufio.Writer, target Target) {
	if err := dstWriter.Flush(); err != nil {
		logrus.Debugf("[Processor] [%s] flush before fallback failed: %v", target.Address, err)
	}
	if _, err := io.Copy(dst, srcReader); err != nil && err != io.EOF {
		logrus.Debugf("[Processor] [%s] fallback copy error: %v", target.Address, err)
	}
}

func (p *Processor) processRequest(dstWriter *bufio.Writer, srcReader *bufio.Reader, target Target) bool {
	request, err := http.ReadRequest(srcReader)
	if err != nil {
		if err == io.EOF || strings.Contains(err.Error(), "use of closed network connection") {
			logrus.Debugf("[Processor] [%s] connection closed", target.Address)
		} else if strings.Contains(err.Error(), "connection reset by peer") {
			logrus.Debugf("[Processor] [%s] connection reset", target.Address)
		} else {
			logrus.Debugf("[Processor] [%s] read request error: %v", target.Address, err)
		}
		return false
	}
	defer request.Body.Close()

	p.reporter.ReportHTTP(target.IP, target.Port)
	p.requestMetrics.RecordHTTPRequest()

	result := p.rewriter.Rewrite(request, target.IP, target.Port)
	if result.FromCache {
		if result.Modified {
			p.requestMetrics.RecordCacheHitModified()
			p.requestMetrics.RecordModifiedRequest()
			logrus.Debugf("[Processor] [%s] UA modified (cached)", target.Address)
		} else {
			p.requestMetrics.RecordCacheHitPassthrough()
			logrus.Debugf("[Processor] [%s] UA not modified (cached)", target.Address)
		}
	} else if result.Modified {
		p.requestMetrics.RecordModifiedRequest()
		logrus.Debugf("[Processor] [%s] %s", target.Address, result.Decision.Reason)
	} else {
		logrus.Debugf("[Processor] [%s] %s", target.Address, result.Decision.Reason)
	}

	if result.Decision.DropConnection {
		logrus.Debugf("[Processor] [%s] dropping connection after firewall bypass hit", target.Address)
		return false
	}

	if err := request.Write(dstWriter); err != nil {
		logrus.Debugf("[Processor] [%s] write request error: %v", target.Address, err)
		return false
	}
	if err := dstWriter.Flush(); err != nil {
		logrus.Debugf("[Processor] [%s] flush after request failed: %v", target.Address, err)
		return false
	}

	return true
}
