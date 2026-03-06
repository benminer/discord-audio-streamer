package discord

import (
	"log/slog"
	"sync/atomic"

	log "github.com/sirupsen/logrus"

	"github.com/bwmarrin/discordgo"
	"github.com/disgoorg/godave"
	"github.com/disgoorg/godave/golibdave"
)

var daveLogger = log.WithField("module", "dave")

// daveSessionAdapter wraps godave.Session to implement discordgo.DaveSession.
// This is needed because godave uses defined types (UserID, ChannelID, Codec)
// while discordgo.DaveSession uses primitive types (string, uint64, int).
type daveSessionAdapter struct {
	session        godave.Session
	encryptFrames  atomic.Uint64 // total frames passed to Encrypt
	encryptErrors  atomic.Uint64 // total Encrypt errors
	transitionDone atomic.Bool   // true after ExecuteTransition with non-zero version
}

func (a *daveSessionAdapter) MaxSupportedProtocolVersion() int {
	return a.session.MaxSupportedProtocolVersion()
}

func (a *daveSessionAdapter) SetChannelID(channelID uint64) {
	daveLogger.Infof("SetChannelID called: %d", channelID)
	a.session.SetChannelID(godave.ChannelID(channelID))
}

func (a *daveSessionAdapter) AssignSsrcToCodec(ssrc uint32, codec int) {
	daveLogger.Infof("AssignSsrcToCodec: ssrc=%d codec=%d", ssrc, codec)
	a.session.AssignSsrcToCodec(ssrc, godave.Codec(codec))
}

func (a *daveSessionAdapter) MaxEncryptedFrameSize(frameSize int) int {
	return a.session.MaxEncryptedFrameSize(frameSize)
}

func (a *daveSessionAdapter) Encrypt(ssrc uint32, frame []byte, encryptedFrame []byte) (int, error) {
	n, err := a.session.Encrypt(ssrc, frame, encryptedFrame)
	total := a.encryptFrames.Add(1)
	if err != nil {
		errCount := a.encryptErrors.Add(1)
		if errCount <= 5 || errCount%500 == 0 {
			daveLogger.Errorf("Encrypt failed (frame %d, errors %d): %v", total, errCount, err)
		}
	} else if total == 1 {
		daveLogger.Infof("First frame encrypted successfully (input=%d bytes, output=%d bytes, transitionDone=%v)", len(frame), n, a.transitionDone.Load())
	} else if total%2500 == 0 { // ~50 seconds
		errCount := a.encryptErrors.Load()
		daveLogger.Debugf("Encrypt stats: %d frames, %d errors, transitionDone=%v", total, errCount, a.transitionDone.Load())
	}
	return n, err
}

func (a *daveSessionAdapter) OnSelectProtocolAck(protocolVersion uint16) {
	daveLogger.Infof("OnSelectProtocolAck: protocolVersion=%d", protocolVersion)
	a.session.OnSelectProtocolAck(protocolVersion)
}

func (a *daveSessionAdapter) OnDavePrepareTransition(transitionID uint16, protocolVersion uint16) {
	daveLogger.Infof("OnDavePrepareTransition: transitionID=%d protocolVersion=%d", transitionID, protocolVersion)
	a.session.OnDavePrepareTransition(transitionID, protocolVersion)
}

func (a *daveSessionAdapter) OnDaveExecuteTransition(transitionID uint16) {
	daveLogger.Infof("OnDaveExecuteTransition: transitionID=%d (DAVE encryption should now be active)", transitionID)
	a.transitionDone.Store(true)
	a.session.OnDaveExecuteTransition(transitionID)
}

func (a *daveSessionAdapter) OnDavePrepareEpoch(epoch int, protocolVersion uint16) {
	daveLogger.Infof("OnDavePrepareEpoch: epoch=%d protocolVersion=%d", epoch, protocolVersion)
	a.session.OnDavePrepareEpoch(epoch, protocolVersion)
}

func (a *daveSessionAdapter) OnDaveMLSExternalSenderPackage(externalSenderPackage []byte) {
	daveLogger.Infof("OnDaveMLSExternalSenderPackage: %d bytes", len(externalSenderPackage))
	a.session.OnDaveMLSExternalSenderPackage(externalSenderPackage)
}

func (a *daveSessionAdapter) OnDaveMLSProposals(proposals []byte) {
	daveLogger.Infof("OnDaveMLSProposals: %d bytes", len(proposals))
	a.session.OnDaveMLSProposals(proposals)
}

func (a *daveSessionAdapter) OnDaveMLSPrepareCommitTransition(transitionID uint16, commitMessage []byte) {
	daveLogger.Infof("OnDaveMLSPrepareCommitTransition: transitionID=%d commitLen=%d", transitionID, len(commitMessage))
	a.session.OnDaveMLSPrepareCommitTransition(transitionID, commitMessage)
}

func (a *daveSessionAdapter) OnDaveMLSWelcome(transitionID uint16, welcomeMessage []byte) {
	daveLogger.Infof("OnDaveMLSWelcome: transitionID=%d welcomeLen=%d", transitionID, len(welcomeMessage))
	a.session.OnDaveMLSWelcome(transitionID, welcomeMessage)
}

func (a *daveSessionAdapter) AddUser(userID string) {
	daveLogger.Debugf("AddUser: %s", userID)
	a.session.AddUser(godave.UserID(userID))
}

func (a *daveSessionAdapter) RemoveUser(userID string) {
	daveLogger.Debugf("RemoveUser: %s", userID)
	a.session.RemoveUser(godave.UserID(userID))
}

// daveCallbacksAdapter wraps discordgo.DaveCallbacks to implement godave.Callbacks.
type daveCallbacksAdapter struct {
	callbacks discordgo.DaveCallbacks
}

func (a *daveCallbacksAdapter) SendMLSKeyPackage(mlsKeyPackage []byte) error {
	return a.callbacks.SendMLSKeyPackage(mlsKeyPackage)
}

func (a *daveCallbacksAdapter) SendMLSCommitWelcome(mlsCommitWelcome []byte) error {
	return a.callbacks.SendMLSCommitWelcome(mlsCommitWelcome)
}

func (a *daveCallbacksAdapter) SendReadyForTransition(transitionID uint16) error {
	return a.callbacks.SendReadyForTransition(transitionID)
}

func (a *daveCallbacksAdapter) SendInvalidCommitWelcome(transitionID uint16) error {
	return a.callbacks.SendInvalidCommitWelcome(transitionID)
}

// NewDaveSessionCreate returns a DaveSessionCreateFunc that creates real DAVE sessions
// using libdave via golibdave bindings.
func NewDaveSessionCreate() discordgo.DaveSessionCreateFunc {
	return func(userID string, callbacks discordgo.DaveCallbacks) discordgo.DaveSession {
		logger := slog.Default().With(slog.String("module", "dave"))
		godaveSession := golibdave.NewSession(
			logger,
			godave.UserID(userID),
			&daveCallbacksAdapter{callbacks: callbacks},
		)
		adapter := &daveSessionAdapter{session: godaveSession}
		daveLogger.Infof("DaveSession created for user %s (maxProtocolVersion=%d)", userID, adapter.MaxSupportedProtocolVersion())
		return adapter
	}
}
