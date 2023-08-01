package logger

import (
	"github.com/rs/zerolog"
	"os"
	"syscall"
)

type ZeroLogger struct {
	logger *zerolog.Logger
}

func NewZeroLogger(filename string) *ZeroLogger {
	fw, _ := os.OpenFile(filename, syscall.O_RDWR|os.O_TRUNC|syscall.O_CREAT, 0666)
	multi := zerolog.MultiLevelWriter(fw, os.Stdout)
	// zerolog.TimeFieldFormat = zerolog.TimeFormatUnixMicro
	zerolog.TimeFieldFormat = "2006-01-02 15:04:05.000"
	logger := zerolog.New(multi).With().Timestamp().Logger()
	return &ZeroLogger{logger: &logger}
}

func (zl *ZeroLogger) Debugf(format string, v ...interface{}) {
	zl.logger.Debug().Msgf(format, v...)
}
func (zl *ZeroLogger) Debugln(msg string) {
	zl.logger.Debug().Msg(msg)
}

func (zl *ZeroLogger) Infof(format string, v ...interface{}) {
	zl.logger.Info().Msgf(format, v...)
}
func (zl *ZeroLogger) Infoln(msg string) {
	zl.logger.Info().Msg(msg)
}

func (zl *ZeroLogger) Warnf(format string, v ...interface{}) {
	zl.logger.Warn().Msgf(format, v...)
}
func (zl *ZeroLogger) Warnln(msg string) {
	zl.logger.Warn().Msg(msg)
}

func (zl *ZeroLogger) Error(msg string, err error) {
	zl.logger.Err(err).Msg(msg)
}
func (zl *ZeroLogger) Errorln(msg string) {
	zl.logger.Error().Msg(msg)
}

func (zl *ZeroLogger) Fatalf(format string, v ...interface{}) {
	zl.logger.Fatal().Msgf(format, v...)
}
func (zl *ZeroLogger) Fatalln(msg string) {
	zl.logger.Fatal().Msg(msg)
}

func (zl *ZeroLogger) Panicf(format string, v ...interface{}) {
	zl.logger.Panic().Msgf(format, v...)
}
func (zl *ZeroLogger) Panicln(msg string) {
	zl.logger.Panic().Msg(msg)
}
