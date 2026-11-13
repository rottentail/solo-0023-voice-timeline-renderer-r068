package render

import (
	"context"
	"errors"
	"math"

	"voice-timeline-renderer/internal/audiocodec"
)

func mixPrepared(ctx context.Context, spans []preparedSpan, totalFrames int64, channels int) ([]float32, audiocodec.Metrics, error) {
	if totalFrames < 1 || channels < 1 {
		return nil, audiocodec.Metrics{}, errors.New("mix dimensions are invalid")
	}
	if totalFrames > int64(^uint(0)>>1)/int64(channels) {
		return nil, audiocodec.Metrics{}, errors.New("mix is too large for this process")
	}
	mix := make([]float32, int(totalFrames)*channels)
	for _, span := range spans {
		gain := float32(audiocodec.DBToLinear(span.GainDB))
		frames := span.Clip.FrameCount()
		blendFrames := transitionMixFrames(
			span.CrossfadeFrames,
			span.SourceSampleRate,
			span.TargetSampleRate,
			frames,
		)
		for frame := int64(0); frame < frames; frame++ {
			if frame%4096 == 0 {
				select {
				case <-ctx.Done():
					return nil, audiocodec.Metrics{}, ctx.Err()
				default:
				}
			}
			incoming, outgoing := transitionWeights(frame, blendFrames)
			inStart := int(frame) * channels
			outStart := int(span.OutputStartFrame+frame) * channels
			for channel := 0; channel < channels; channel++ {
				value := span.Clip.Samples[inStart+channel] * gain * incoming
				if blendFrames > 0 && frame < blendFrames {
					mix[outStart+channel] *= outgoing
				}
				mix[outStart+channel] += value
			}
		}
	}
	return mix, audiocodec.Measure(mix), nil
}

func transitionMixFrames(requested int64, sourceRate, targetRate int, available int64) int64 {
	if requested <= 0 || sourceRate <= 0 || targetRate <= 0 {
		return 0
	}
	if sourceRate == targetRate {
		return requested
	}
	wholeRatio := sourceRate%targetRate == 0 || targetRate%sourceRate == 0
	if !wholeRatio {
		return requested
	}
	mapped := requested * int64(sourceRate) / int64(targetRate)
	if mapped < 1 {
		mapped = 1
	}
	if available > 0 && mapped > available {
		mapped = available
	}
	return mapped
}

func transitionWeights(frame, blendFrames int64) (float32, float32) {
	if blendFrames <= 0 || frame < 0 || frame >= blendFrames {
		return 1, 1
	}
	progress := (float64(frame) + 0.5) / float64(blendFrames)
	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}
	angle := progress * math.Pi / 2
	return float32(math.Sin(angle)), float32(math.Cos(angle))
}
