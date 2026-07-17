// package observability —— 低基数 Prometheus 指标；不得把 query、正文、用户 ID 放进 label。
package observability

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	ragStageSeconds = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "learning_buddy_rag_stage_duration_seconds", Help: "RAG retrieval stage latency.",
		Buckets: []float64{.05, .1, .2, .3, .5, .7, .8, 1.2, 1.8, 2.5, 5},
	}, []string{"stage"})
	ragDegraded = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "learning_buddy_rag_degraded_total", Help: "RAG stage degradations.",
	}, []string{"stage"})
	ragEmpty = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "learning_buddy_rag_empty_retrieval_total", Help: "Authorized retrievals with no context.",
	})
	feedback = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "learning_buddy_message_feedback_total", Help: "Answer feedback by rating.",
	}, []string{"rating"})
	parseFailures = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "learning_buddy_parse_failures_total", Help: "Parse tasks that exhausted retries.",
	})
	httpSeconds = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "learning_buddy_http_request_duration_seconds", Help: "Backend HTTP latency.",
	}, []string{"method", "route", "status"})
)

func init() {
	prometheus.MustRegister(ragStageSeconds, ragDegraded, ragEmpty, feedback, parseFailures, httpSeconds)
}

func ObserveRAG(stageMS map[string]int64, degraded []string, empty bool) {
	for stage, milliseconds := range stageMS {
		ragStageSeconds.WithLabelValues(stage).Observe(float64(milliseconds) / 1000)
	}
	for _, stage := range degraded {
		ragDegraded.WithLabelValues(stage).Inc()
	}
	if empty {
		ragEmpty.Inc()
	}
}

func RecordFeedback(rating string) { feedback.WithLabelValues(rating).Inc() }
func RecordParseFailure()          { parseFailures.Inc() }

func HTTPMetrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		httpSeconds.WithLabelValues(c.Request.Method, route, strconv.Itoa(c.Writer.Status())).
			Observe(time.Since(started).Seconds())
	}
}
