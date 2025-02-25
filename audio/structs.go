package audio

type PlaybackNotificationType string

const (
	PlaybackLoading      PlaybackNotificationType = "loading"
	PlaybackLoaded       PlaybackNotificationType = "loaded"
	PlaybackLoadError    PlaybackNotificationType = "load_error"
	PlaybackLoadCanceled PlaybackNotificationType = "load_canceled"
	PlaybackStarted      PlaybackNotificationType = "started"
	PlaybackPaused       PlaybackNotificationType = "paused"
	PlaybackResumed      PlaybackNotificationType = "resumed"
	PlaybackCompleted    PlaybackNotificationType = "completed"
	PlaybackStopped      PlaybackNotificationType = "stopped"
	PlaybackError        PlaybackNotificationType = "error"
)

type PlaybackNotification struct {
	Error      *error
	VideoID    *string
	Event      PlaybackNotificationType
	LoadResult *LoadResult
}
