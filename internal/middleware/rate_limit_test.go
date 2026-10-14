package middleware

import (
	"testing"
	"time"
)

func TestIPLimiter_Allow(t *testing.T) {
	start := time.Now()
	l := newIPLimiter(1, 1, start)

	steps := []struct {
		name string
		ip   string
		at   time.Duration
		want bool
	}{
		{name: "消耗唯一的令牌", ip: "1.1.1.1", want: true},
		{name: "令牌耗尽", ip: "1.1.1.1", want: false},
		{name: "其他 IP 不受影响", ip: "2.2.2.2", want: true},
		{name: "一秒后补充一个令牌", ip: "1.1.1.1", at: time.Second, want: true},
	}

	for _, step := range steps {
		if got := l.allow(step.ip, start.Add(step.at)); got != step.want {
			t.Fatalf("%s: allow = %v, want %v", step.name, got, step.want)
		}
	}
}

func TestIPLimiter_SweepOnlyFullBuckets(t *testing.T) {
	start := time.Now()
	l := newIPLimiter(1, 2, start)

	l.allow("idle", start)
	l.allow("busy", start.Add(sweepInterval-time.Millisecond))

	// 触发清理：idle 早已回满，busy 刚消耗过令牌
	l.allow("trigger", start.Add(sweepInterval))

	if _, ok := l.limiters["idle"]; ok {
		t.Fatal("已回满的令牌桶应当被清理")
	}
	if _, ok := l.limiters["busy"]; !ok {
		t.Fatal("未回满的令牌桶不能被清理")
	}
}
