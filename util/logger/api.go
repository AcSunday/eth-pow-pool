package logger

import (
	"fmt"
)

// 开箱即用api

func init() {
	err := InitTimeLogger("./demo.log", "./demo_err.log", 7, 3600)
	if err != nil {
		panic(err)
	}
}

// 格式化调用信息
func formatCaller(s string) string {
	_, fileName, lineNo := getCaller(3)
	return fmt.Sprintf("%s:%d %s", fileName, lineNo, s)
}

func Debug(s string, args ...interface{}) {
	s = formatCaller(s)
	SugarLogger.Debugf(s, args...)
}

func Info(s string, args ...interface{}) {
	s = formatCaller(s)
	SugarLogger.Infof(s, args...)
}

func Warn(s string, args ...interface{}) {
	s = formatCaller(s)
	SugarLogger.Warnf(s, args...)
}

func Error(s string, args ...interface{}) {
	s = formatCaller(s)
	SugarLogger.Errorf(s, args...)
}

func Panic(s string, args ...interface{}) {
	s = formatCaller(s)
	SugarLogger.Panicf(s, args...)
}

func Fatal(s string, args ...interface{}) {
	s = formatCaller(s)
	SugarLogger.Fatalf(s, args...)
}

// log data flush to disk
func Sync() {
	if err := SugarLogger.Sync(); err != nil {
		Error("Log data sync failed, err: %v", err)
		return
	}
	Debug("Log data sync to file is complete")
}
