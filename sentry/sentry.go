package sentry

import (
	"context"
	"os"

	sentry "github.com/getsentry/sentry-go"
	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

func Init() {
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:              os.Getenv("SENTRY_DSN"),
		TracesSampleRate: 1.0,
	}); err != nil {
		log.Fatalf("sentry.Init: %s", err)
	}
}

func GetSentryClient() *sentry.Client {
	return sentry.GetHubFromContext(context.Background()).Client()
}

func GetSentryGin() gin.HandlerFunc {
	return sentrygin.New(sentrygin.Options{})
}

func ReportError(err error) {
	sentry.CaptureException(err)
}

func SetContext(name string, value map[string]interface{}) {
	sentry.GetHubFromContext(context.Background()).ConfigureScope(func(scope *sentry.Scope) {
		scope.SetContext(name, value)
		scope.SetTag("release", os.Getenv("RELEASE"))
	})
}

func ReportMessage(message string) {
	sentry.CaptureMessage(message)
}

func ReportFatal(err error) {
	sentry.CaptureException(err)
}
