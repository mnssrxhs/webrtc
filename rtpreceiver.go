// +build !js

package webrtc

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/srtp/v2"
	"github.com/pion/webrtc/v3/internal/util"
)

const (
	IctFECSSRCAttr  = "fec-ssrc"
	IctRTXSSRCAttr  = "rtx-ssrc"
	IctRTXTrackAttr = "rtxtrack"
	IctFECTrackAttr = "fectrack"
)

// trackStreams maintains a mapping of RTP/RTCP streams to a specific track
// a RTPReceiver may contain multiple streams if we are dealing with Multicast
type trackStreams struct {
	track *TrackRemote

	rtpReadStream  *srtp.ReadStreamSRTP
	rtpInterceptor interceptor.RTPReader

	rtcpReadStream  *srtp.ReadStreamSRTCP
	rtcpInterceptor interceptor.RTCPReader

	fecTrack *trackStreams
	rtxTrack *trackStreams
}

// RTPReceiver allows an application to inspect the receipt of a TrackRemote
type RTPReceiver struct {
	kind      RTPCodecType
	transport *DTLSTransport

	tracks []trackStreams

	closed, received chan interface{}
	mu               sync.RWMutex

	// A reference to the associated api object
	api *API
}

// NewRTPReceiver constructs a new RTPReceiver
func (api *API) NewRTPReceiver(kind RTPCodecType, transport *DTLSTransport) (*RTPReceiver, error) {
	if transport == nil {
		return nil, errRTPReceiverDTLSTransportNil
	}

	r := &RTPReceiver{
		kind:      kind,
		transport: transport,
		api:       api,
		closed:    make(chan interface{}),
		received:  make(chan interface{}),
		tracks:    []trackStreams{},
	}

	return r, nil
}

// Transport returns the currently-configured *DTLSTransport or nil
// if one has not yet been configured
func (r *RTPReceiver) Transport() *DTLSTransport {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.transport
}

// GetParameters describes the current configuration for the encoding and
// transmission of media on the receiver's track.
func (r *RTPReceiver) GetParameters() RTPParameters {
	return r.api.mediaEngine.getRTPParametersByKind(r.kind, []RTPTransceiverDirection{RTPTransceiverDirectionRecvonly})
}

// Track returns the RtpTransceiver TrackRemote
func (r *RTPReceiver) Track() *TrackRemote {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.tracks) != 1 {
		return nil
	}
	return r.tracks[0].track
}

// FecTrack returns RtpTransceiver's track's fec track
func (r *RTPReceiver) FecTrack() *TrackRemote {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.tracks) != 1 {
		return nil
	}
	if r.tracks[0].fecTrack != nil {
		return r.tracks[0].fecTrack.track
	}
	return nil
}

// RtxTrack returns RtpTransceiver's track's rtx track
func (r *RTPReceiver) RtxTrack() *TrackRemote {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.tracks) != 1 {
		return nil
	}
	if r.tracks[0].rtxTrack != nil {
		return r.tracks[0].rtxTrack.track
	}
	return nil
}

// Tracks returns the RtpTransceiver tracks
// A RTPReceiver to support Simulcast may now have multiple tracks
func (r *RTPReceiver) Tracks() []*TrackRemote {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var tracks []*TrackRemote
	for i := range r.tracks {
		tracks = append(tracks, r.tracks[i].track)
	}
	return tracks
}

// Receive initialize the track and starts all the transports
func (r *RTPReceiver) Receive(parameters RTPReceiveParameters) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	select {
	case <-r.received:
		return errRTPReceiverReceiveAlreadyCalled
	default:
	}
	defer close(r.received)

	if len(parameters.Encodings) == 1 && parameters.Encodings[0].SSRC != 0 {
		t := trackStreams{
			track: newTrackRemote(
				r.kind,
				parameters.Encodings[0].SSRC,
				"",
				r,
			),
		}

		globalParams := r.GetParameters()
		codec := RTPCodecCapability{}
		if len(globalParams.Codecs) != 0 {
			codec = globalParams.Codecs[0].RTPCodecCapability
		}

		streamInfo := createStreamInfo("", parameters.Encodings[0].SSRC, 0, codec, globalParams.HeaderExtensions)

		// fec & rtx
		if parameters.Encodings[0].FecSSRC != 0 {
			streamInfo.Attributes.Set(IctFECSSRCAttr, uint32(parameters.Encodings[0].FecSSRC))
		}
		if parameters.Encodings[0].RtxSSRC != 0 {
			streamInfo.Attributes.Set(IctRTXSSRCAttr, uint32(parameters.Encodings[0].RtxSSRC))
		}

		var err error
		if t.rtpReadStream, t.rtpInterceptor, t.rtcpReadStream, t.rtcpInterceptor, err = r.streamsForSSRC(parameters.Encodings[0].SSRC, streamInfo); err != nil {
			return err
		}

		r.tracks = append(r.tracks, t)

		// fec & rtx
		if parameters.Encodings[0].FecSSRC != 0 {
			t := trackStreams{
				track: newTrackRemote(
					r.kind,
					SSRC(parameters.Encodings[0].FecSSRC),
					"",
					r,
				),
			}
			t.track.isFec = true
			streamInfo := createStreamInfo("", SSRC(parameters.Encodings[0].FecSSRC), 0, codec, globalParams.HeaderExtensions)
			streamInfo.Attributes.Set(IctFECTrackAttr, 1)
			var err error
			if t.rtpReadStream, t.rtpInterceptor, t.rtcpReadStream, t.rtcpInterceptor, err = r.streamsForSSRC(SSRC(parameters.Encodings[0].FecSSRC), streamInfo); err != nil {
				return err
			}
			r.tracks[0].fecTrack = &t
		}
		if parameters.Encodings[0].RtxSSRC != 0 {
			t := trackStreams{
				track: newTrackRemote(
					r.kind,
					SSRC(parameters.Encodings[0].RtxSSRC),
					"",
					r,
				),
			}
			t.track.isRtx = true
			streamInfo := createStreamInfo("", SSRC(parameters.Encodings[0].RtxSSRC), 0, codec, globalParams.HeaderExtensions)
			streamInfo.Attributes.Set(IctRTXTrackAttr, 1)
			var err error
			if t.rtpReadStream, t.rtpInterceptor, t.rtcpReadStream, t.rtcpInterceptor, err = r.streamsForSSRC(SSRC(parameters.Encodings[0].RtxSSRC), streamInfo); err != nil {
				return err
			}
			r.tracks[0].rtxTrack = &t
		}

	} else {
		for _, encoding := range parameters.Encodings {
			r.tracks = append(r.tracks, trackStreams{
				track: newTrackRemote(
					r.kind,
					0,
					encoding.RID,
					r,
				),
			})
		}
	}

	return nil
}

// Read reads incoming RTCP for this RTPReceiver
func (r *RTPReceiver) Read(b []byte) (n int, a interceptor.Attributes, err error) {
	select {
	case <-r.received:
		return r.tracks[0].rtcpInterceptor.Read(b, a)
	case <-r.closed:
		return 0, nil, io.ErrClosedPipe
	}
}

// ReadSimulcast reads incoming RTCP for this RTPReceiver for given rid
func (r *RTPReceiver) ReadSimulcast(b []byte, rid string) (n int, a interceptor.Attributes, err error) {
	select {
	case <-r.received:
		for _, t := range r.tracks {
			if t.track != nil && t.track.rid == rid {
				return t.rtcpInterceptor.Read(b, a)
			}
		}
		return 0, nil, fmt.Errorf("%w: %s", errRTPReceiverForRIDTrackStreamNotFound, rid)
	case <-r.closed:
		return 0, nil, io.ErrClosedPipe
	}
}

// ReadRTCP is a convenience method that wraps Read and unmarshal for you.
// It also runs any configured interceptors.
func (r *RTPReceiver) ReadRTCP() ([]rtcp.Packet, interceptor.Attributes, error) {
	b := make([]byte, receiveMTU)
	i, attributes, err := r.Read(b)
	if err != nil {
		return nil, nil, err
	}

	pkts, err := rtcp.Unmarshal(b[:i])
	if err != nil {
		return nil, nil, err
	}

	return pkts, attributes, nil
}

// ReadSimulcastRTCP is a convenience method that wraps ReadSimulcast and unmarshal for you
func (r *RTPReceiver) ReadSimulcastRTCP(rid string) ([]rtcp.Packet, interceptor.Attributes, error) {
	b := make([]byte, receiveMTU)
	i, attributes, err := r.ReadSimulcast(b, rid)
	if err != nil {
		return nil, nil, err
	}

	pkts, err := rtcp.Unmarshal(b[:i])
	return pkts, attributes, err
}

func (r *RTPReceiver) haveReceived() bool {
	select {
	case <-r.received:
		return true
	default:
		return false
	}
}

// Stop irreversibly stops the RTPReceiver
func (r *RTPReceiver) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var err error

	select {
	case <-r.closed:
		return err
	default:
	}

	select {
	case <-r.received:
		for i := range r.tracks {
			errs := []error{}

			if r.tracks[i].rtcpReadStream != nil {
				errs = append(errs, r.tracks[i].rtcpReadStream.Close())
			}

			if r.tracks[i].rtpReadStream != nil {
				errs = append(errs, r.tracks[i].rtpReadStream.Close())
			}

			err = util.FlattenErrs(errs)
		}
	default:
	}

	close(r.closed)
	return err
}

func (r *RTPReceiver) streamsForTrack(t *TrackRemote) *trackStreams {
	for i := range r.tracks {
		if r.tracks[i].track == t {
			return &r.tracks[i]
		}
		if r.tracks[i].fecTrack != nil && r.tracks[i].fecTrack.track == t {
			return r.tracks[i].fecTrack
		}

		if r.tracks[i].rtxTrack != nil && r.tracks[i].rtxTrack.track == t {
			return r.tracks[i].rtxTrack
		}
	}
	return nil
}

// readRTP should only be called by a track, this only exists so we can keep state in one place
func (r *RTPReceiver) readRTP(b []byte, reader *TrackRemote) (n int, a interceptor.Attributes, err error) {
	<-r.received
	if t := r.streamsForTrack(reader); t != nil {
		return t.rtpInterceptor.Read(b, a)
	}

	return 0, nil, fmt.Errorf("%w: %d", errRTPReceiverWithSSRCTrackStreamNotFound, reader.SSRC())
}

// receiveForRid is the sibling of Receive expect for RIDs instead of SSRCs
// It populates all the internal state for the given RID
func (r *RTPReceiver) receiveForRid(rid string, params RTPParameters, ssrc SSRC) (*TrackRemote, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i := range r.tracks {
		if r.tracks[i].track.RID() == rid {
			r.tracks[i].track.mu.Lock()
			r.tracks[i].track.kind = r.kind
			r.tracks[i].track.codec = params.Codecs[0]
			r.tracks[i].track.params = params
			r.tracks[i].track.ssrc = ssrc
			streamInfo := createStreamInfo("", ssrc, params.Codecs[0].PayloadType, params.Codecs[0].RTPCodecCapability, params.HeaderExtensions)
			r.tracks[i].track.mu.Unlock()

			var err error
			if r.tracks[i].rtpReadStream, r.tracks[i].rtpInterceptor, r.tracks[i].rtcpReadStream, r.tracks[i].rtcpInterceptor, err = r.streamsForSSRC(ssrc, streamInfo); err != nil {
				return nil, err
			}

			return r.tracks[i].track, nil
		}
	}

	return nil, fmt.Errorf("%w: %d", errRTPReceiverForSSRCTrackStreamNotFound, ssrc)
}

func (r *RTPReceiver) streamsForSSRC(ssrc SSRC, streamInfo interceptor.StreamInfo) (*srtp.ReadStreamSRTP, interceptor.RTPReader, *srtp.ReadStreamSRTCP, interceptor.RTCPReader, error) {
	srtpSession, err := r.transport.getSRTPSession()
	if err != nil {
		return nil, nil, nil, nil, err
	}

	rtpReadStream, err := srtpSession.OpenReadStream(uint32(ssrc))
	if err != nil {
		return nil, nil, nil, nil, err
	}

	rtpInterceptor := r.api.interceptor.BindRemoteStream(&streamInfo, interceptor.RTPReaderFunc(func(in []byte, a interceptor.Attributes) (n int, attributes interceptor.Attributes, err error) {
		n, err = rtpReadStream.Read(in)
		return n, a, err
	}))

	srtcpSession, err := r.transport.getSRTCPSession()
	if err != nil {
		return nil, nil, nil, nil, err
	}

	rtcpReadStream, err := srtcpSession.OpenReadStream(uint32(ssrc))
	if err != nil {
		return nil, nil, nil, nil, err
	}

	rtcpInterceptor := r.api.interceptor.BindRTCPReader(interceptor.RTPReaderFunc(func(in []byte, a interceptor.Attributes) (n int, attributes interceptor.Attributes, err error) {
		n, err = rtcpReadStream.Read(in)
		return n, a, err
	}))

	return rtpReadStream, rtpInterceptor, rtcpReadStream, rtcpInterceptor, nil
}

// SetReadDeadline sets the max amount of time the RTCP stream will block before returning. 0 is forever.
func (r *RTPReceiver) SetReadDeadline(t time.Time) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if err := r.tracks[0].rtcpReadStream.SetReadDeadline(t); err != nil {
		return err
	}
	return nil
}

// SetReadDeadlineSimulcast sets the max amount of time the RTCP stream for a given rid will block before returning. 0 is forever.
func (r *RTPReceiver) SetReadDeadlineSimulcast(deadline time.Time, rid string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, t := range r.tracks {
		if t.track != nil && t.track.rid == rid {
			return t.rtcpReadStream.SetReadDeadline(deadline)
		}
	}
	return fmt.Errorf("%w: %s", errRTPReceiverForRIDTrackStreamNotFound, rid)
}

// setRTPReadDeadline sets the max amount of time the RTP stream will block before returning. 0 is forever.
// This should be fired by calling SetReadDeadline on the TrackRemote
func (r *RTPReceiver) setRTPReadDeadline(deadline time.Time, reader *TrackRemote) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if t := r.streamsForTrack(reader); t != nil {
		return t.rtpReadStream.SetReadDeadline(deadline)
	}
	return fmt.Errorf("%w: %d", errRTPReceiverWithSSRCTrackStreamNotFound, reader.SSRC())
}
