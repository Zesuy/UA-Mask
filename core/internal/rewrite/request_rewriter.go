package rewrite

import "net/http"

type Result struct {
	Decision  Decision
	FromCache bool
	Modified  bool
}

type RequestRewriter struct {
	engine *Engine
	cache  *UACache
}

func NewRequestRewriter(engine *Engine, cache *UACache) *RequestRewriter {
	return &RequestRewriter{
		engine: engine,
		cache:  cache,
	}
}

func (r *RequestRewriter) Rewrite(request *http.Request, _ string, _ int) Result {
	originUA := request.Header.Get("User-Agent")
	if originUA == "" {
		return Result{
			Decision: Decision{
				FinalUA:   originUA,
				Reason:    ReasonNoUserAgent,
				Cacheable: false,
			},
		}
	}

	if finalUA, ok := r.cache.Get(originUA); ok {
		request.Header.Set("User-Agent", finalUA)
		return Result{
			Decision: Decision{
				Matched:   finalUA != originUA,
				Replace:   finalUA != originUA,
				FinalUA:   finalUA,
				Cacheable: true,
			},
			FromCache: true,
			Modified:  finalUA != originUA,
		}
	}

	decision := r.engine.Evaluate(originUA)

	if decision.Replace {
		request.Header.Set("User-Agent", decision.FinalUA)
	}

	if decision.Cacheable {
		r.cache.Add(originUA, decision.FinalUA)
	}

	return Result{
		Decision: decision,
		Modified: decision.Replace,
	}
}
