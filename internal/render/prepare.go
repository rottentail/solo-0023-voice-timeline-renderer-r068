package render

import (
	"context"
	"errors"

	"voice-timeline-renderer/internal/audiocodec"
	"voice-timeline-renderer/internal/domain"
)

type SourceInput struct {
	Asset domain.SourceAsset
	WAV   []byte
}

type preparedSpan struct {
	Index            int
	SourceID         string
	SourceDigest     string
	SourceStartFrame int64
	SourceEndFrame   int64
	OutputStartFrame int64
	OutputEndFrame   int64
	OutputFrames     int64
	CrossfadeFrames  int64
	SourceSampleRate int
	TargetSampleRate int
	GainDB           float64
	ChannelAction    string
	Clip             *audiocodec.Clip
}

func prepareSpans(
	ctx context.Context,
	plan domain.Timeline,
	sources map[string]SourceInput,
) ([]preparedSpan, int64, error) {
	decoded := make(map[string]*audiocodec.Clip, len(sources))
	prepared := make([]preparedSpan, 0, len(plan.Spans))
	cursor := int64(0)
	for index, span := range plan.Spans {
		source, ok := sources[span.SourceID]
		if !ok {
			return nil, 0, errors.New("timeline references a missing source")
		}
		clip, ok := decoded[span.SourceID]
		if !ok {
			parsed, _, err := audiocodec.DecodeWAV(source.WAV)
			if err != nil {
				return nil, 0, err
			}
			clip = parsed
			decoded[span.SourceID] = parsed
		}
		selected, err := audiocodec.SliceFrames(ctx, clip, span.SourceStartFrame, span.SourceEndFrame)
		if err != nil {
			return nil, 0, err
		}
		resampled, err := audiocodec.Resample(ctx, selected, plan.SampleRate)
		if err != nil {
			return nil, 0, err
		}
		mapped, action, err := audiocodec.MapChannels(ctx, resampled, plan.OutputChannels)
		if err != nil {
			return nil, 0, err
		}
		frames := mapped.FrameCount()
		if frames < 1 {
			return nil, 0, errors.New("span produced no output frames")
		}

		blendFrames := transitionBlendFrames(
			span.CrossfadeFrames,
			source.Asset.SampleRate,
			plan.SampleRate,
			frames,
		)
		start := cursor
		if index > 0 {
			start -= blendFrames
		}
		if start < 0 {
			return nil, 0, errors.New("crossfade produced a negative output position")
		}
		end := start + frames
		cursor = end

		prepared = append(prepared, preparedSpan{
			Index:            index,
			SourceID:         span.SourceID,
			SourceDigest:     source.Asset.Digest,
			SourceStartFrame: span.SourceStartFrame,
			SourceEndFrame:   span.SourceEndFrame,
			OutputStartFrame: start,
			OutputEndFrame:   end,
			OutputFrames:     frames,
			CrossfadeFrames:  span.CrossfadeFrames,
			SourceSampleRate: source.Asset.SampleRate,
			TargetSampleRate: plan.SampleRate,
			GainDB:           span.GainDB,
			ChannelAction:    action,
			Clip:             mapped,
		})
	}
	return prepared, cursor, nil
}

// transitionBlendFrames maps exact-rate transitions back onto their source
// grid. Keeping integral conversions on that grid avoids fractional source
// positions while a span is prepared for mixing.
func transitionBlendFrames(requested int64, sourceRate, targetRate int, available int64) int64 {
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
