package discordgo

import (
	"encoding/binary"
	"encoding/json"
	"fmt"

	"github.com/gorilla/websocket"
)

// DaveSession is the interface for DAVE E2EE session handling.
// Implement using golibdave.NewSession from github.com/disgoorg/godave.
// The VoiceConnection will call these methods when DAVE opcodes are received,
// and the session will call back via DaveCallbacks to send responses.
type DaveSession interface {
	// MaxSupportedProtocolVersion returns the maximum supported DAVE version.
	MaxSupportedProtocolVersion() int

	// SetChannelID sets the channel ID for this session.
	SetChannelID(channelID uint64)

	// AssignSsrcToCodec maps a given SSRC to a specific codec (1 = Opus).
	AssignSsrcToCodec(ssrc uint32, codec int)

	// MaxEncryptedFrameSize returns the max size of an encrypted frame.
	MaxEncryptedFrameSize(frameSize int) int

	// Encrypt encrypts an Opus frame. Returns the number of bytes written.
	Encrypt(ssrc uint32, frame []byte, encryptedFrame []byte) (int, error)

	// OnSelectProtocolAck handles SELECT_PROTOCOL_ACK (opcode 4) dave_protocol_version.
	OnSelectProtocolAck(protocolVersion uint16)

	// OnDavePrepareTransition handles DAVE_PROTOCOL_PREPARE_TRANSITION (opcode 21).
	OnDavePrepareTransition(transitionID uint16, protocolVersion uint16)

	// OnDaveExecuteTransition handles DAVE_PROTOCOL_EXECUTE_TRANSITION (opcode 22).
	OnDaveExecuteTransition(protocolVersion uint16)

	// OnDavePrepareEpoch handles DAVE_PROTOCOL_PREPARE_EPOCH (opcode 24).
	OnDavePrepareEpoch(epoch int, protocolVersion uint16)

	// OnDaveMLSExternalSenderPackage handles opcode 25.
	OnDaveMLSExternalSenderPackage(externalSenderPackage []byte)

	// OnDaveMLSProposals handles opcode 27.
	OnDaveMLSProposals(proposals []byte)

	// OnDaveMLSPrepareCommitTransition handles opcode 29.
	OnDaveMLSPrepareCommitTransition(transitionID uint16, commitMessage []byte)

	// OnDaveMLSWelcome handles opcode 30.
	OnDaveMLSWelcome(transitionID uint16, welcomeMessage []byte)

	// AddUser adds a user to the MLS group.
	AddUser(userID string)

	// RemoveUser removes a user from the MLS group.
	RemoveUser(userID string)
}

// DaveCallbacks are called by the DaveSession to send messages back to Discord.
// VoiceConnection implements this interface.
type DaveCallbacks interface {
	SendMLSKeyPackage(mlsKeyPackage []byte) error
	SendMLSCommitWelcome(mlsCommitWelcome []byte) error
	SendReadyForTransition(transitionID uint16) error
	SendInvalidCommitWelcome(transitionID uint16) error
}

// DaveSessionCreateFunc creates a DaveSession for a voice connection.
// userID is the bot's user ID. callbacks is the VoiceConnection itself.
type DaveSessionCreateFunc func(userID string, callbacks DaveCallbacks) DaveSession

// DAVE voice gateway opcodes
const (
	voiceOpDavePrepareTransition        = 21
	voiceOpDaveExecuteTransition        = 22
	voiceOpDaveTransitionReady          = 23
	voiceOpDavePrepareEpoch             = 24
	voiceOpDaveMLSExternalSenderPackage = 25
	voiceOpDaveMLSKeyPackage            = 26
	voiceOpDaveMLSProposals             = 27
	voiceOpDaveMLSCommitWelcome         = 28
	voiceOpDaveMLSPrepareCommitTransition = 29
	voiceOpDaveMLSWelcome               = 30
	voiceOpDaveMLSInvalidCommitWelcome  = 31
)

// SendMLSKeyPackage sends opcode 26 (binary) to the voice gateway.
func (v *VoiceConnection) SendMLSKeyPackage(mlsKeyPackage []byte) error {
	return v.sendDaveBinary(voiceOpDaveMLSKeyPackage, mlsKeyPackage)
}

// SendMLSCommitWelcome sends opcode 28 (binary) to the voice gateway.
func (v *VoiceConnection) SendMLSCommitWelcome(mlsCommitWelcome []byte) error {
	return v.sendDaveBinary(voiceOpDaveMLSCommitWelcome, mlsCommitWelcome)
}

// SendReadyForTransition sends opcode 23 (JSON) to the voice gateway.
func (v *VoiceConnection) SendReadyForTransition(transitionID uint16) error {
	type readyData struct {
		TransitionID uint16 `json:"transition_id"`
	}
	type readyOp struct {
		Op   int       `json:"op"`
		Data readyData `json:"d"`
	}
	return v.sendDaveJSON(readyOp{voiceOpDaveTransitionReady, readyData{transitionID}})
}

// SendInvalidCommitWelcome sends opcode 31 (JSON) to the voice gateway.
func (v *VoiceConnection) SendInvalidCommitWelcome(transitionID uint16) error {
	type invalidData struct {
		TransitionID uint16 `json:"transition_id"`
	}
	type invalidOp struct {
		Op   int         `json:"op"`
		Data invalidData `json:"d"`
	}
	return v.sendDaveJSON(invalidOp{voiceOpDaveMLSInvalidCommitWelcome, invalidData{transitionID}})
}

// sendDaveBinary sends a binary DAVE message over the voice WebSocket.
// Format: 1 byte opcode + payload
func (v *VoiceConnection) sendDaveBinary(opcode int, payload []byte) error {
	if v.wsConn == nil {
		return fmt.Errorf("no voice websocket connection")
	}

	buf := make([]byte, 1+len(payload))
	buf[0] = byte(opcode)
	copy(buf[1:], payload)

	v.wsMutex.Lock()
	err := v.wsConn.WriteMessage(websocket.BinaryMessage, buf)
	v.wsMutex.Unlock()
	return err
}

// sendDaveJSON sends a JSON DAVE message over the voice WebSocket.
func (v *VoiceConnection) sendDaveJSON(data interface{}) error {
	if v.wsConn == nil {
		return fmt.Errorf("no voice websocket connection")
	}

	v.wsMutex.Lock()
	err := v.wsConn.WriteJSON(data)
	v.wsMutex.Unlock()
	return err
}

// onDaveTextEvent handles JSON-encoded DAVE opcodes from the voice gateway.
func (v *VoiceConnection) onDaveTextEvent(op int, rawData json.RawMessage) {
	if v.DaveSession == nil {
		return
	}

	switch op {
	case voiceOpDavePrepareTransition:
		var d struct {
			ProtocolVersion uint16 `json:"protocol_version"`
			TransitionID    uint16 `json:"transition_id"`
		}
		if err := json.Unmarshal(rawData, &d); err != nil {
			v.log(LogError, "DAVE opcode 21 unmarshal error: %s", err)
			return
		}
		v.log(LogDebug, "DAVE PrepareTransition: transition_id=%d protocol_version=%d", d.TransitionID, d.ProtocolVersion)
		v.DaveSession.OnDavePrepareTransition(d.TransitionID, d.ProtocolVersion)

	case voiceOpDaveExecuteTransition:
		var d struct {
			TransitionID uint16 `json:"transition_id"`
		}
		if err := json.Unmarshal(rawData, &d); err != nil {
			v.log(LogError, "DAVE opcode 22 unmarshal error: %s", err)
			return
		}
		v.log(LogDebug, "DAVE ExecuteTransition: transition_id=%d", d.TransitionID)
		v.DaveSession.OnDaveExecuteTransition(d.TransitionID)

	case voiceOpDavePrepareEpoch:
		var d struct {
			ProtocolVersion uint16 `json:"protocol_version"`
			Epoch           int    `json:"epoch"`
		}
		if err := json.Unmarshal(rawData, &d); err != nil {
			v.log(LogError, "DAVE opcode 24 unmarshal error: %s", err)
			return
		}
		v.log(LogDebug, "DAVE PrepareEpoch: epoch=%d protocol_version=%d", d.Epoch, d.ProtocolVersion)
		v.DaveSession.OnDavePrepareEpoch(d.Epoch, d.ProtocolVersion)
	}
}

// onDaveBinaryEvent handles binary-encoded DAVE opcodes from the voice gateway.
// Binary format from server: 2 bytes seq (big-endian) + 1 byte opcode + payload
func (v *VoiceConnection) onDaveBinaryEvent(message []byte) {
	if v.DaveSession == nil {
		return
	}

	if len(message) < 3 {
		v.log(LogWarning, "DAVE binary message too short: %d bytes", len(message))
		return
	}

	// seq := binary.BigEndian.Uint16(message[:2])
	op := int(message[2])
	payload := message[3:]

	switch op {
	case voiceOpDaveMLSExternalSenderPackage: // 25
		v.log(LogDebug, "DAVE MLSExternalSenderPackage: %d bytes", len(payload))
		v.DaveSession.OnDaveMLSExternalSenderPackage(payload)

	case voiceOpDaveMLSProposals: // 27
		v.log(LogDebug, "DAVE MLSProposals: %d bytes", len(payload))
		v.DaveSession.OnDaveMLSProposals(payload)

	case voiceOpDaveMLSPrepareCommitTransition: // 29
		if len(payload) < 2 {
			v.log(LogWarning, "DAVE opcode 29 payload too short")
			return
		}
		transitionID := binary.BigEndian.Uint16(payload[:2])
		commitMessage := payload[2:]
		v.log(LogDebug, "DAVE MLSPrepareCommitTransition: transition_id=%d commit=%d bytes", transitionID, len(commitMessage))
		v.DaveSession.OnDaveMLSPrepareCommitTransition(transitionID, commitMessage)

	case voiceOpDaveMLSWelcome: // 30
		if len(payload) < 2 {
			v.log(LogWarning, "DAVE opcode 30 payload too short")
			return
		}
		transitionID := binary.BigEndian.Uint16(payload[:2])
		welcomeMessage := payload[2:]
		v.log(LogDebug, "DAVE MLSWelcome: transition_id=%d welcome=%d bytes", transitionID, len(welcomeMessage))
		v.DaveSession.OnDaveMLSWelcome(transitionID, welcomeMessage)

	default:
		v.log(LogDebug, "unknown DAVE binary opcode %d, %d bytes", op, len(payload))
	}
}
