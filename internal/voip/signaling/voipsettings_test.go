package signaling

import "testing"

const voipSettingsSample = `{"aec":{"algorithm":"aec","mode":"2"},"bwe":{"bwe_impl":"8"},"encode":{"complexity":"5","frame_ms":"60","min_bitrate":"4200","use_mlow_codec_v1":"true"},"rc":{"dtx":"1","init_bitrate":"15000","target_bitrate":"24000"},"voip_settings_version":{"release_type":"1","version_number":"152548"}}`

func TestParseVoipSettingsSample(t *testing.T) {
	vs, err := ParseVoipSettings([]byte(voipSettingsSample))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !vs.Present || !vs.UseMlowCodecV1 {
		t.Fatalf("want present mlow settings, got %+v", vs)
	}
	if vs.FrameMs != 60 || vs.TargetBitrate != 24000 {
		t.Fatalf("want frame_ms 60 and target_bitrate 24000, got %+v", vs)
	}
	if vs.CodecName() != CodecMLow {
		t.Fatalf("want codec mlow, got %s", vs.CodecName())
	}
}

func TestParseVoipSettingsExplicitOpus(t *testing.T) {
	vs, err := ParseVoipSettings([]byte(`{"encode":{"use_mlow_codec_v1":"false"}}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if vs.UseMlowCodecV1 {
		t.Fatal("literal false must disable mlow")
	}
	if vs.CodecName() != CodecOpus {
		t.Fatalf("want codec opus, got %s", vs.CodecName())
	}
}

func TestParseVoipSettingsEmptyDefaultsToMlow(t *testing.T) {
	for _, raw := range [][]byte{nil, {}, []byte("  \n")} {
		vs, err := ParseVoipSettings(raw)
		if err != nil {
			t.Fatalf("empty blob must not error: %v", err)
		}
		if vs.Present {
			t.Fatal("empty blob must not be marked present")
		}
		if vs.CodecName() != CodecMLow {
			t.Fatalf("want codec mlow for empty blob, got %s", vs.CodecName())
		}
	}
}

func TestParseVoipSettingsMalformedErrors(t *testing.T) {
	if _, err := ParseVoipSettings([]byte("{not json")); err == nil {
		t.Fatal("malformed json must error")
	}
}

func TestParseVoipSettingsAbsentIntsAreZero(t *testing.T) {
	vs, err := ParseVoipSettings([]byte(`{"encode":{"use_mlow_codec_v1":"true","frame_ms":"garbage"}}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if vs.FrameMs != 0 || vs.TargetBitrate != 0 {
		t.Fatalf("absent or unparseable ints must be 0, got %+v", vs)
	}
}

func TestCodecNameNilSafe(t *testing.T) {
	var vs *VoipSettings
	if vs.CodecName() != CodecMLow {
		t.Fatal("nil settings must select mlow")
	}
}
