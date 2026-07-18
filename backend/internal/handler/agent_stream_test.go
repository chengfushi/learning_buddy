package handler

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAgentStreamCompletionError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		done   bool
		answer string
		err    error
		want   string
	}{
		{name: "complete", done: true, answer: "有据回答"},
		{name: "missing terminal", answer: "部分回答", want: "回答生成中断，请重试"},
		{name: "read failure", done: true, answer: "部分回答", err: errors.New("reset"), want: "回答生成中断，请重试"},
		{name: "blank answer", done: true, answer: " \n\t", want: "当前知识库未返回有效回答，请重试"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.want, agentStreamCompletionError(test.done, test.answer, test.err))
		})
	}
}
