package main

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/sirupsen/logrus"
)

var (
	HTTP_METHOD = []string{"GET", "POST", "HEAD", "PUT", "DELETE", "OPTIONS", "TRACE", "CONNECT", "PATCH"}
)

type HTTPHandler struct {
	config    *Config
	stats     *Stats
	cache     *lru.Cache[string, string]
	fwManager *FirewallSetManager

	// bufio.Reader 池
	bufioReaderPool sync.Pool
	// bufio.Writer 池
	bufioWriterPool sync.Pool
}

func NewHTTPHandler(config *Config, stats *Stats, cache *lru.Cache[string, string], fwManager *FirewallSetManager) *HTTPHandler {
	h := &HTTPHandler{
		config:    config,
		stats:     stats,
		cache:     cache,
		fwManager: fwManager,
	}

	// 初始化 Reader 池
	h.bufioReaderPool = sync.Pool{
		New: func() any {
			return bufio.NewReaderSize(nil, config.BufferSize)
		},
	}
	// 初始化 Writer 池
	h.bufioWriterPool = sync.Pool{
		New: func() any {
			return bufio.NewWriterSize(nil, config.BufferSize)
		},
	}

	return h
}

// peek HTTP
func (h *HTTPHandler) isHTTP(reader *bufio.Reader) (bool, error) {
	buf, err := reader.Peek(7)
	if err != nil {
		if strings.Contains(err.Error(), "EOF") {
			logrus.Debugf("Peek EOF: %s", err.Error())
		} else {
			logrus.Debugf("Peek error: %s", err.Error())
		}
		return false, err
	}
	hint := string(buf)
	is_http := false
	for _, v := range HTTP_METHOD {
		if strings.HasPrefix(hint, v) {
			is_http = true
			break
		}
	}
	return is_http, nil
}

// 构造新 User-Agent 字符串
func (h *HTTPHandler) buildNewUA(originUA string, replacementUA string, uaRegexp *regexp.Regexp, enablePartialReplace bool) string {
	if enablePartialReplace && uaRegexp != nil {
		// 启用部分替换：使用正则替换
		newUaHearder := uaRegexp.ReplaceAllString(originUA, replacementUA)
		return newUaHearder
	}
	// 默认完整替换
	return replacementUA
}

// ModifyAndForward 是核心处理函数，负责修改 User-Agent 并转发数据
func (h *HTTPHandler) ModifyAndForward(dst net.Conn, src net.Conn, destAddrPort string, destIP string, destPort int) {
	srcReader := h.bufioReaderPool.Get().(*bufio.Reader)
	srcReader.Reset(src)
	defer h.bufioReaderPool.Put(srcReader)

	dstWriter := h.bufioWriterPool.Get().(*bufio.Writer)
	dstWriter.Reset(dst)
	defer func() {
		if err := dstWriter.Flush(); err != nil {
			logrus.Debugf("[%s] Final flush error on exit: %v", destAddrPort, err)
		}
		h.bufioWriterPool.Put(dstWriter)
	}()

	logrus.Debugf("[%s] HTTP detected, processing with go prase", destAddrPort)

	for {
		is_http, err := h.isHTTP(srcReader)
		//检测失败
		if err != nil {
			if err == io.EOF || strings.Contains(err.Error(), "use of closed network connection") {
				logrus.Debugf("[%s] Connection closed (EOF or closed in loop)", destAddrPort)
			} else {
				logrus.Debugf("[%s] isHTTP check in loop error: %v", destAddrPort, err)
			}

			// 退出前尝试刷新剩余数据
			if err_flush := dstWriter.Flush(); err_flush != nil {
				logrus.Debugf("[%s] Flush error before fallback (isHTTP err): %v", destAddrPort, err_flush)
			}
			// 回退到io.copy
			if _, err := io.Copy(dst, srcReader); err != nil && err != io.EOF {
				logrus.Debugf("[%s] Fallback copy error: %v", destAddrPort, err)
			}
			return
		}

		if !is_http {
			logrus.Debugf("[%s] Protocol switch detected. Changing to direct relay mode.", destAddrPort)
			// 刷新已缓冲的数据
			if err_flush := dstWriter.Flush(); err_flush != nil {
				logrus.Debugf("[%s] Flush error before fallback (isHTTP err): %v", destAddrPort, err_flush)
			}
			if h.config.EnableFirewallUABypass {
				h.fwManager.Add(destIP, destPort, h.config.FirewallIPSetName, h.config.FirewallType, 600)
			}
			if _, err := io.Copy(dst, srcReader); err != nil && err != io.EOF {
				logrus.Debugf("[%s] Fallback copy error: %v", destAddrPort, err)
			}
			return
		}

		// 3. 使用 Go 标准库解析 HTTP 头部
		request, err := http.ReadRequest(srcReader)
		if err != nil {
			if err == io.EOF || strings.Contains(err.Error(), "use of closed network connection") {
				logrus.Debugf("[%s] Connection closed (EOF or closed)", destAddrPort)
			} else if strings.Contains(err.Error(), "connection reset by peer") {
				logrus.Debugf("[%s] Connection reset", destAddrPort)
			} else {
				logrus.Debugf("[%s] HTTP read request error: %v", destAddrPort, err)
			}
			return // 结束此连接的处理
		}

		h.stats.IncHttpRequests()

		// 4. 获取 User-Agent
		uaStr := request.Header.Get("User-Agent")
		uaFound := uaStr != ""

		if !uaFound {
			logrus.Debugf("[%s] No User-Agent header, skip modification.", destAddrPort)
		} else {
			if finalUA, ok := h.cache.Get(uaStr); ok {
				// UA 缓存
				request.Header.Set("User-Agent", finalUA)
				if finalUA != uaStr {
					h.stats.IncCacheHits()
					h.stats.IncModifiedRequests()
					logrus.Debugf("[%s] UA modified (cached): %s -> %s", destAddrPort, uaStr, finalUA)
				} else {
					h.stats.IncCacheHitNoModify()
					logrus.Debugf("[%s] UA not modified (cached): %s", destAddrPort, uaStr)
				}
			} else {
				// 未命中缓存
				var shouldReplace bool
				var matchReason string

				// 1. 检查白名单 (最高优先级)
				isFirewallWhitelisted := false
				if len(h.config.FirewallUAWhitelist) > 0 {
					for _, fw_keyword := range h.config.FirewallUAWhitelist {
						if strings.Contains(uaStr, fw_keyword) {
							isFirewallWhitelisted = true
							break
						}
					}
				}
				if isFirewallWhitelisted {
					logrus.Debugf("[%s] Hit Firewall UA Whitelist: %s", destAddrPort, uaStr)
					h.fwManager.Add(destIP, destPort, h.config.FirewallIPSetName, h.config.FirewallType, 86400)
					if h.config.FirewallDropOnMatch {
						logrus.Debugf("[%s] FirewallDropOnMatch enabled, dropping connection for protocol switch bypass.", destAddrPort)
						return
					}
					shouldReplace = false
					matchReason = "Hit Firewall UA Whitelist"

				} else {
					isInWhiteList := false
					for _, v := range h.config.Whitelist {
						if v == uaStr {
							isInWhiteList = true
							break
						}
					}

					if isInWhiteList {
						shouldReplace = false
						matchReason = "Hit User-Agent Whitelist"
					} else {
						// 2. 根据模式进行匹配 (使用 h.config)
						if h.config.ForceReplace {
							// 强制模式
							shouldReplace = true
							matchReason = "Force Replace Mode"
						} else if h.config.EnableRegex {
							// 正则模式
							if h.config.UARegexp != nil && h.config.UARegexp.MatchString(uaStr) {
								shouldReplace = true
								matchReason = "Hit User-Agent Pattern"
							} else {
								shouldReplace = false
								matchReason = "Not Hit User-Agent Pattern"
							}
						} else {
							// 默认：关键词模式
							shouldReplace = false
							matchReason = "Not Hit User-Agent Keywords"
							for _, keyword := range h.config.KeywordsList {
								if strings.Contains(uaStr, keyword) {
									shouldReplace = true
									matchReason = "Hit User-Agent Keyword"
									break
								}
							}
						}
					}
				}
				// 3. 处理日志和缓存
				if !shouldReplace {
					logrus.Debugf("[%s] %s: %s. ", destAddrPort, matchReason, uaStr)
					if !isFirewallWhitelisted {
						h.cache.Add(uaStr, uaStr) // 缓存不修改的UA
					}
				} else {
					logrus.Debugf("[%s] %s: %s", destAddrPort, matchReason, uaStr)
				}

				// 根据 shouldReplace 标志执行操作
				if shouldReplace {
					// 调用 buildNewUA 来获取最终的 UA 字符串
					finalUA := h.buildNewUA(uaStr, h.config.UserAgent, h.config.UARegexp, h.config.EnablePartialReplace)

					request.Header.Set("User-Agent", finalUA)
					h.stats.IncModifiedRequests()
					if !isFirewallWhitelisted {
						h.cache.Add(uaStr, finalUA) // 缓存修改的UA
					}

					if h.config.ForceReplace {
						logrus.Debugf("[%s] UA modified (forced): %s -> %s", destAddrPort, uaStr, finalUA)
					} else {
						if h.config.EnablePartialReplace && finalUA != h.config.UserAgent {
							logrus.Debugf("[%s] UA partially modified: %s -> %s", destAddrPort, uaStr, finalUA)
						} else {
							logrus.Debugf("[%s] UA fully modified: %s -> %s", destAddrPort, uaStr, finalUA)
						}
					}
				}
			}
		}

		// 6. 写回目标
		if err := request.Write(dstWriter); err != nil {
			logrus.Debugf("[%s] HTTP write request error: %v", destAddrPort, err)
			request.Body.Close()
			return
		}
		// 7. 刷新缓冲区，确保请求头立即发送
		if err := dstWriter.Flush(); err != nil {
			logrus.Debugf("[%s] Flush error after writing request: %v", destAddrPort, err)
			request.Body.Close()
			return
		}

		// 8. 关闭 Body，准备读取下一个 Keep-Alive 请求
		request.Body.Close()
		bodySize := request.ContentLength
		logrus.Debugf("[%s] Request processed, body size: %d. Waiting for next request...", destAddrPort, bodySize)
	}
}
