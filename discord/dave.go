package discord

import (
	"log/slog"

	"github.com/bwmarrin/discordgo"
	"github.com/disgoorg/godave"
	"github.com/disgoorg/godave/golibdave"
)

// daveSessionAdapter wraps godave.Session to implement discordgo.DaveSession.
// This is needed because godave uses defined types (UserID, ChannelID, Codec)
// while discordgo.DaveSession uses primitive types (string, uint64, int).
type daveSessionAdapter struct {
	session godave.Session
}

func (a *daveSessionAdapter) MaxSupportedProtocolVersion() int {
	return a.session.MaxSupportedProtocolVersion()
}

func (a *daveSessionAdapter) SetChannelID(channelID uint64) {
	a.session.SetChannelID(godave.ChannelID(channelID))
}

func (a *daveSessionAdapter) AssignSsrcToCodec(ssrc uint32, codec int) {
	a.session.AssignSsrcToCodec(ssrc, godave.Codec(codec))
}

func (a *daveSessionAdapter) MaxEncryptedFrameSize(frameSize int) int {
	return a.session.MaxEncryptedFrameSize(frameSize)
}

func (a *daveSessionAdapter) Encrypt(ssrc uint32, frame []byte, encryptedFrame []byte) (int, error) {
	return a.session.Encrypt(ssrc, frame, encryptedFrame)
}

func (a *daveSessionAdapter) OnSelectProtocolAck(protocolVersion uint16) {
	a.session.OnSelectProtocolAck(protocolVersion)
}

func (a *daveSessionAdapter) OnDavePrepareTransition(transitionID uint16, protocolVersion uint16) {
	a.session.OnDavePrepareTransition(transitionID, protocolVersion)
}

func (a *daveSessionAdapter) OnDaveExecuteTransition(protocolVersion uint16) {
	a.session.OnDaveExecuteTransition(protocolVersion)
}

func (a *daveSessionAdapter) OnDavePrepareEpoch(epoch int, protocolVersion uint16) {
	a.session.OnDavePrepareEpoch(epoch, protocolVersion)
}

func (a *daveSessionAdapter) OnDaveMLSExternalSenderPackage(externalSenderPackage []byte) {
	a.session.OnDaveMLSExternalSenderPackage(externalSenderPackage)
}

func (a *daveSessionAdapter) OnDaveMLSProposals(proposals []byte) {
	a.session.OnDaveMLSProposals(proposals)
}

func (a *daveSessionAdapter) OnDaveMLSPrepareCommitTransition(transitionID uint16, commitMessage []byte) {
	a.session.OnDaveMLSPrepareCommitTransition(transitionID, commitMessage)
}

func (a *daveSessionAdapter) OnDaveMLSWelcome(transitionID uint16, welcomeMessage []byte) {
	a.session.OnDaveMLSWelcome(transitionID, welcomeMessage)
}

func (a *daveSessionAdapter) AddUser(userID string) {
	a.session.AddUser(godave.UserID(userID))
}

func (a *daveSessionAdapter) RemoveUser(userID string) {
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
		return &daveSessionAdapter{session: godaveSession}
	}
}
