package logger

import (
	"fmt"
)

// 开箱即用api

func init() {
	// 不推荐使用init方式自动初始化logger对象
	//err := InitTimeLogger("./demo.log", "./demo_err.log", 7, 3600)
	//if err != nil {
	//	panic(err)
	//}
}

// 格式化调用信息
func formatCaller(s string) string {
	_, fileName, lineNo := getCaller(3)
	return fmt.Sprintf("%s:%d %s", fileName, lineNo, s)
}

// Debug级别日志
func Debug(s string, args ...interface{}) {
	if DEBUG {
		s = formatCaller(s)
		SugarLogger.Debugf(s, args...)
	}
}

// Info级别日志
func Info(s string, args ...interface{}) {
	s = formatCaller(s)
	SugarLogger.Infof(s, args...)
}

// Warning级别日志
func Warn(s string, args ...interface{}) {
	s = formatCaller(s)
	SugarLogger.Warnf(s, args...)
}

// Error级别日志
func Error(s string, args ...interface{}) {
	s = formatCaller(s)
	SugarLogger.Errorf(s, args...)
}

// Panic级别日志
func Panic(s string, args ...interface{}) {
	s = formatCaller(s)
	SugarLogger.Panicf(s, args...)
}

// Fatal级别日志，会exit程序
func Fatal(s string, args ...interface{}) {
	s = formatCaller(s)
	SugarLogger.Fatalf(s, args...)
}

// log data flush to disk
//  日志数据落盘
func Sync() {
	if err := SugarLogger.Sync(); err != nil {
		Error("Log data sync failed, err: %v", err)
		return
	}
	Debug("Log data sync to file is complete")
}
