// ————————————————————————————————————————————————————————————————————————————
// remotewriter —— 报表远端输出:POST JSON —— 文件总结
//
// Writer 接口的 HTTP 实现:SetReportWriter(NewRemoteWriter(url))
// 之后,Metrics 每分钟的 StatReport 会序列化成 JSON POST 到
// 该端点(监控/看板系统消费)。5 秒超时;非 200 记错误日志,
// 不重试、不阻塞主流程(报表丢一条无所谓)。
// ————————————————————————————————————————————————————————————————————————————
package stat

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	// httpTimeout 上报超时(报表低频,给足 5s)。
	httpTimeout = time.Second * 5
	// jsonContentType 上报的 Content-Type。
	jsonContentType = "application/json; charset=utf-8"
)

// ErrWriteFailed is an error that indicates failed to submit a StatReport.
// 远端返回非 200 时的错误。
var ErrWriteFailed = errors.New("submit failed")

// A RemoteWriter is a writer to write StatReport.
// HTTP POST 版报表后端。
type RemoteWriter struct {
	endpoint string
}

// NewRemoteWriter returns a RemoteWriter.
// 创建远端 writer(endpoint 为上报 URL)。
func NewRemoteWriter(endpoint string) Writer {
	return &RemoteWriter{
		endpoint: endpoint,
	}
}

// Write 序列化报表并 POST;失败/非 200 返回错误
// (由 metrics.writeReport 记日志,不影响业务)。
func (rw *RemoteWriter) Write(report *StatReport) error {
	bs, err := json.Marshal(report)
	if err != nil {
		return err
	}

	client := &http.Client{
		Timeout: httpTimeout,
	}
	resp, err := client.Post(rw.endpoint, jsonContentType, bytes.NewReader(bs))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logx.Errorf("write report failed, code: %d, reason: %s", resp.StatusCode, resp.Status)
		return ErrWriteFailed
	}

	return nil
}
