package signaling

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

const (
	CodecMLow = "mlow"
	CodecOpus = "opus"
)

// VoipSettings is the codec-relevant subset of the server's <voip_settings> JSON
// blob, delivered inline on an inbound <offer> and in the <ack> of an outbound
// offer. The call stays on MLow unless the server explicitly disables it.
type VoipSettings struct {
	UseMlowCodecV1 bool
	FrameMs        int
	TargetBitrate  int
	Present        bool
}

// CodecName maps the settings to the codec the server selected for the call.
// Only a present, explicit use_mlow_codec_v1=false selects Opus.
func (vs *VoipSettings) CodecName() string {
	if vs == nil || !vs.Present || vs.UseMlowCodecV1 {
		return CodecMLow
	}
	return CodecOpus
}

// ParseVoipSettings parses the <voip_settings> JSON content into the codec-relevant
// subset. An empty blob yields the MLow default; malformed JSON is an error. The
// values are stringly-typed on the wire ("true"/"60"), so each is converted
// explicitly; use_mlow_codec_v1 is true unless the key is the literal "false".
func ParseVoipSettings(raw []byte) (*VoipSettings, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return &VoipSettings{UseMlowCodecV1: true}, nil
	}
	var doc struct {
		Encode struct {
			UseMlowCodecV1 string `json:"use_mlow_codec_v1"`
			FrameMs        string `json:"frame_ms"`
		} `json:"encode"`
		RC struct {
			TargetBitrate string `json:"target_bitrate"`
		} `json:"rc"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse voip_settings: %w", err)
	}
	return &VoipSettings{
		UseMlowCodecV1: doc.Encode.UseMlowCodecV1 != "false",
		FrameMs:        atoiOrZero(doc.Encode.FrameMs),
		TargetBitrate:  atoiOrZero(doc.RC.TargetBitrate),
		Present:        true,
	}, nil
}

func atoiOrZero(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}
