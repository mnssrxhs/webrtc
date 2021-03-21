package main

import (
	"fmt"

	"github.com/pion/webrtc/v3"
)

func main() {

	peerConnection, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	})
	if err != nil {
		panic(err)
	}

	peerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio)
	peerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo)

	offer, err := peerConnection.CreateOffer(&webrtc.OfferOptions{
		OfferAnswerOptions: webrtc.OfferAnswerOptions{VoiceActivityDetection: true},
	})

	fmt.Println("offer ", offer.SDP)
}
