package main

import (
	"context"
	"log/slog"

	"github.com/art-injener/satellite-scout/internal/registry"
)

// rfFrameRecorder пишет принятые кадры в радиопрофиль через реестр.
type rfFrameRecorder struct {
	reg *registry.Registry
}

func (r rfFrameRecorder) RecordFrame(norad int, freqHz int64, demodID string, tsMs int64) {
	if r.reg == nil {
		return
	}
	if err := r.reg.ApplyRFFrame(context.Background(), norad, freqHz, demodID, tsMs); err != nil {
		slog.Warn("rf profile: frame", slogKeyError, err)
	}
}
