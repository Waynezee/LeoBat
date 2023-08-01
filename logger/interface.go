package logger

type Logger interface {
	Infof(format string, v ...interface{})
	Infoln(msg string)
	Debugf(format string, v ...interface{})
	Debugln(msg string)
	Warnf(format string, v ...interface{})
	Warnln(msg string)
	Error(msg string, err error)
	Errorln(msg string)
}
