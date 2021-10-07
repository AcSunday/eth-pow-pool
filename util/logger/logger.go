package logger

import (
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/natefinch/lumberjack"

	rotatelogs "github.com/lestrrat-go/file-rotatelogs"
)

// Logger对象功能集

var (
	// zap的Logger对象
	SugarLogger *zap.SugaredLogger

	infoLevelFunc = zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
		return lvl <= zapcore.InfoLevel
	})
	errorLevelFunc = zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
		return lvl >= zapcore.WarnLevel
	})
)

// 初始化Logger日志对象 文件按时间间隔切割
//  filepath, errFilePath info与err文件路径, saveDays 保存最近几(天)的文件, cutInterval 切割时间间隔(秒)
func InitTimeLogger(filepath, errFilePath string, saveDays, cutInterval time.Duration) (err error) {
	//if SugarLogger != nil {
	//	return errors.New("Logger object has been initialized. ")
	//}

	encoder := getEncoder()
	// 区分info日志文件与 err日志文件
	infoWriter := getTimeDivisionWriter(filepath, saveDays, cutInterval)
	errorWriter := getTimeDivisionWriter(errFilePath, saveDays, cutInterval)
	core := zapcore.NewTee(
		zapcore.NewCore(encoder, zapcore.AddSync(infoWriter), infoLevelFunc),
		zapcore.NewCore(encoder, zapcore.AddSync(errorWriter), errorLevelFunc),
	)
	logger := zap.New(core)
	SugarLogger = logger.Sugar()
	return
}

// 初始化Logger日志对象 文件按大小切割
//  filepath 文件路径, maxSize 文件最大大小（MB）,
//  backupNums 保留的备份文件个数, saveDays 保存最近几天的文件, isZip 是否归档
func InitSizeLogger(filepath string, maxSize, backupNums, saveDays int, isZip bool) (err error) {
	if SugarLogger != nil {
		return errors.New("Logger object has been initialized. ")
	}

	writeSyncer := getCutSizeWriter(filepath, maxSize, backupNums, saveDays, isZip)
	encoder := getEncoder()
	core := zapcore.NewCore(encoder, writeSyncer, infoLevelFunc)

	logger := zap.New(core, zap.AddCaller())
	SugarLogger = logger.Sugar()
	return
}

// 获取zap编码器
func getEncoder() zapcore.Encoder {
	encoderConfig := zap.NewProductionEncoderConfig()
	//encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderConfig.EncodeTime = func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendString(t.Format("2006-01-02 15:04:05"))
	}
	encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
	//return zapcore.NewJSONEncoder(encoderConfig) // json格式日志
	return zapcore.NewConsoleEncoder(encoderConfig) // console格式日志
}

// 获取 按大小切割写入器
//  filepath 文件路径, maxSize 文件最大大小（MB）,
//  backupNums 保留的备份文件个数, saveDays 保存最近几天的文件, isZip 是否归档
func getCutSizeWriter(filepath string, maxSize, backupNums, saveDays int, isZip bool) zapcore.WriteSyncer {
	lumberJackLogger := &lumberjack.Logger{
		Filename:   filepath,
		MaxSize:    maxSize,
		MaxBackups: backupNums,
		MaxAge:     saveDays,
		Compress:   isZip,
	}
	return zapcore.AddSync(lumberJackLogger)
}

// 获取 按时间切割写入器
//  filepath 文件路径, saveDays 保存最近几(天)的文件, cutInterval 切割时间间隔(秒)
func getTimeDivisionWriter(filepath string, saveDays, cutInterval time.Duration) io.Writer {
	hook, err := rotatelogs.New(
		fmt.Sprintf("%s%s", filepath, "%Y%m%d-%H%M%S"),
		rotatelogs.WithLinkName(filepath),
		rotatelogs.WithMaxAge(time.Hour*24*saveDays),
		rotatelogs.WithRotationTime(time.Second*cutInterval),
	)

	if err != nil {
		panic(err)
	}
	return hook
}

// 获取调用位置，skip代表调用层级
func getCaller(skip int) (funcName, fileName string, lineNo int) {
	pc, fileName, lineNo, ok := runtime.Caller(skip)
	if !ok {
		Error("runtime.Caller() failed")
		return
	}
	funcName = runtime.FuncForPC(pc).Name()
	funcName = strings.Split(funcName, ".")[1]
	return
}
