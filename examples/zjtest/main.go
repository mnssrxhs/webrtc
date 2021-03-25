package main

import (
	"fmt"

	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v3"
)

var (
	peerConnectionConfig webrtc.Configuration
	me                   webrtc.MediaEngine
	// meStereo             webrtc.MediaEngine
	se webrtc.SettingEngine

	// rttThreshold        int
	// rrFracLostThreshold uint8
)

const (
	iosH264Fmtp     = "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f"
	H264RtpPayload  = 100
	RtxVideoPayload = 101
	RtxAudioPayload = 112
)

var ans = `v=0
o=- 5669932068473563629 2 IN IP4 127.0.0.1
s=-
t=0 0
a=group:BUNDLE 0 1 2
a=msid-semantic: WMS 12345
m=audio 9 UDP/TLS/RTP/SAVPF 111 96
c=IN IP4 0.0.0.0
a=rtcp:9 IN IP4 0.0.0.0
a=ice-ufrag:MH7k
a=ice-pwd:lPYEayCSmm4V/M0uzFkWeAvu
a=ice-options:trickle
a=fingerprint:sha-256 7E:5D:A8:5C:7C:1E:C7:C8:64:3E:AF:2A:A0:FE:BB:EA:24:9A:00:1E:3B:3F:E7:57:F7:FE:CD:E6:BB:5B:80:F6
a=setup:active
a=mid:0
a=sendonly
a=msid:12345 audio
a=rtcp-mux
a=rtpmap:111 opus/48000/2
a=rtcp-fb:111 transport-cc
a=fmtp:111 minptime=10;useinbandfec=1
a=rtpmap:96 flexfec-03/48000/2
a=ssrc-group:FEC-FR 2910981775 4265190772
a=ssrc:2910981775 cname:OjKsuCgKjuPYR38e
a=ssrc:2910981775 msid:12345 audio
a=ssrc:2910981775 mslabel:12345
a=ssrc:2910981775 label:audio
a=ssrc:4265190772 cname:OjKsuCgKjuPYR38e
a=ssrc:4265190772 msid:12345 audio
a=ssrc:4265190772 mslabel:12345
a=ssrc:4265190772 label:audio
m=video 9 UDP/TLS/RTP/SAVPF 100 126 101
c=IN IP4 0.0.0.0
a=rtcp:9 IN IP4 0.0.0.0
a=ice-ufrag:MH7k
a=ice-pwd:lPYEayCSmm4V/M0uzFkWeAvu
a=ice-options:trickle
a=fingerprint:sha-256 7E:5D:A8:5C:7C:1E:C7:C8:64:3E:AF:2A:A0:FE:BB:EA:24:9A:00:1E:3B:3F:E7:57:F7:FE:CD:E6:BB:5B:80:F6
a=setup:active
a=mid:1
a=sendonly
a=msid:12345 video
a=rtcp-mux
a=rtcp-rsize
a=rtpmap:100 H264/90000
a=rtcp-fb:100 transport-cc
a=rtcp-fb:100 nack
a=rtcp-fb:100 nack pli
a=fmtp:100 level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f
a=rtpmap:126 flexfec-03/90000
a=fmtp:126 repair-window=10000000
a=rtpmap:101 rtx/90000
a=fmtp:101 apt=100
a=ssrc-group:FID 4009161198 3664636240
a=ssrc-group:FEC-FR 4009161198 4259192753
a=ssrc:4009161198 cname:OjKsuCgKjuPYR38e
a=ssrc:4009161198 msid:12345 video
a=ssrc:4009161198 mslabel:12345
a=ssrc:4009161198 label:video
a=ssrc:3664636240 cname:OjKsuCgKjuPYR38e
a=ssrc:3664636240 msid:12345 video
a=ssrc:3664636240 mslabel:12345
a=ssrc:3664636240 label:video
a=ssrc:4259192753 cname:OjKsuCgKjuPYR38e
a=ssrc:4259192753 msid:12345 video
a=ssrc:4259192753 mslabel:12345
a=ssrc:4259192753 label:video
`

// m=video 9 UDP/TLS/RTP/SAVPF 100 126 101
// c=IN IP4 0.0.0.0
// a=rtcp:9 IN IP4 0.0.0.0
// a=ice-ufrag:MH7k
// a=ice-pwd:lPYEayCSmm4V/M0uzFkWeAvu
// a=ice-options:trickle
// a=fingerprint:sha-256 7E:5D:A8:5C:7C:1E:C7:C8:64:3E:AF:2A:A0:FE:BB:EA:24:9A:00:1E:3B:3F:E7:57:F7:FE:CD:E6:BB:5B:80:F6
// a=setup:active
// a=mid:2
// a=inactive
// a=rtcp-mux
// a=rtcp-rsize
// a=rtpmap:100 H264/90000
// a=rtcp-fb:100 transport-cc
// a=rtcp-fb:100 nack
// a=rtcp-fb:100 nack pli
// a=fmtp:100 level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f
// a=rtpmap:126 flexfec-03/90000
// a=fmtp:126 repair-window=10000000
// a=rtpmap:101 rtx/90000
// a=fmtp:101 apt=100

var (
	videoRTCPFeedback = []webrtc.RTCPFeedback{{"transport-cc", ""}, {"nack", ""}, {"nack", "pli"}}
	H264c             = webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{webrtc.MimeTypeH264, 90000, 0, iosH264Fmtp, videoRTCPFeedback},
		PayloadType:        100,
	}

	audioRTCPFeedback = []webrtc.RTCPFeedback{{"transport-cc", ""}}
	Opusc             = webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{webrtc.MimeTypeOpus, 48000, 2, "minptime=10;useinbandfec=1", audioRTCPFeedback},
		PayloadType:        111,
	}
)

func CreateApi() *webrtc.API {
	// todo : register interceptors
	i := &interceptor.Registry{}
	if err := webrtc.ConfigureRTCPReports(i); err != nil {
		// log.WithError(err).Error("ConfigureRTCPReports failed")
	}
	api := webrtc.NewAPI(webrtc.WithMediaEngine(&me), webrtc.WithSettingEngine(se), webrtc.WithInterceptorRegistry(i))
	return api
}

func createMeAndSe() {
	me = webrtc.MediaEngine{}
	me.RegisterCodec(H264c, webrtc.RTPCodecTypeVideo)
	// add fec codec
	fecc := webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{"flexfec-03", 90000, 0, "repair-window=10000000", nil},
		PayloadType:        126,
	}
	me.RegisterCodec(fecc, webrtc.RTPCodecTypeVideo)
	// add rtx codec
	rtxc := webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{"video/rtx", 90000, 0, "apt=100", nil},
		PayloadType:        101,
	}
	me.RegisterCodec(rtxc, webrtc.RTPCodecTypeVideo)

	Opusc.RTCPFeedback = append(Opusc.RTCPFeedback, webrtc.RTCPFeedback{Type: webrtc.TypeRTCPFBNACK})
	me.RegisterCodec(Opusc, webrtc.RTPCodecTypeAudio)

	// add audio fec codec
	fecac := webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{"flexfec-03", 48000, 2, "", nil},
		PayloadType:        96,
	}
	me.RegisterCodec(fecac, webrtc.RTPCodecTypeAudio)

	se = webrtc.SettingEngine{}
	se.SetLite(true)

	// exclude docker network interface

	se.DisableSRTPReplayProtection(true)
	// se.GenerateMulticastDNSCandidates(false)
	// se.SetICEMulticastDNSMode(ice.MulticastDNSMode(ice.MulticastDNSModeDisabled))
	// api = webrtc.NewAPI(webrtc.WithMediaEngine(&me), webrtc.WithSettingEngine(se))
}

func main() {
	createMeAndSe()
	api := CreateApi()

	peerConnection, err := api.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				// URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	})
	if err != nil {
		panic(err)
	}

	peerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly})
	peerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly})

	offer, err := peerConnection.CreateOffer(&webrtc.OfferOptions{
		OfferAnswerOptions: webrtc.OfferAnswerOptions{VoiceActivityDetection: true},
	})

	fmt.Println("offer ", offer.SDP, " err ", err)
	peerConnection.SetLocalDescription(offer)

	ansSdp := webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  ans,
	}
	err = peerConnection.SetRemoteDescription(ansSdp)
	if err != nil {
		fmt.Println("set answer failed", err)
		return
	}

	audioRTCPFeedback := []webrtc.RTCPFeedback{{"transport-cc", ""}}
	Opusc := webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{webrtc.MimeTypeOpus, 48000, 2, "minptime=10;useinbandfec=1", audioRTCPFeedback},
		PayloadType:        111,
	}
	audio, err := webrtc.NewTrackLocalStaticRTP(Opusc.RTPCodecCapability, webrtc.RTPCodecTypeAudio.String(), "1234")
	peerConnection.AddTrack(audio)
	offer, err = peerConnection.CreateOffer(&webrtc.OfferOptions{
		OfferAnswerOptions: webrtc.OfferAnswerOptions{VoiceActivityDetection: true},
	})

	fmt.Println("offer ", offer.SDP, "err", err)

}
