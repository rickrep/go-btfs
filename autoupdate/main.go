package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/TRON-US/go-btfs-api"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

var log = initLogger("update.log").Sugar()

// Log print initialization, get *zap.Logger Info.
func initLogger(logPath string) *zap.Logger {
	// lumberjack.Logger is already safe for concurrent use, so we don't need to
	// lock it.
	w := zapcore.AddSync(&lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    128, // megabytes
		MaxBackups: 30,
		MaxAge:     7, // days
		Compress:   true,
	})
	encoderConfig := zap.NewProductionEncoderConfig()
	// time format
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderConfig),
		zapcore.NewMultiWriteSyncer(zapcore.AddSync(os.Stdout), w), // this line enables log outputs to multiple destinations: log file/stdout
		zap.InfoLevel,
	)
	logger := zap.New(core, zap.AddStacktrace(zap.ErrorLevel))
	return logger
}

// rollback rolls back a failed auto update attempt.
func rollback(wg *sync.WaitGroup, defaultProjectPath, defaultDownloadPath, url string, daemonArgs []string) {
	defer func() {
		wg.Done()
	}()

	// Check if the BTFS daemon server is up every 5 seconds, checked a total of five times.
	for i := 0; i < 5; i++ {
		time.Sleep(time.Second * 5)
		sh := shell.NewShell(url)
		if sh.IsUp() {
			log.Info("BTFS node started successfully!")
			return
		}
	}

	log.Info("BTFS node failed to start, rollback begin!")

	// Select binary files and configure file path based on operating system.
	currentConfigPath, backupConfigPath, _,
		btfsBinaryPath, btfsBackupPath, _,
		frmBinaryPath, frmBackupPath, _,
		err := getProjectPath(defaultProjectPath, defaultDownloadPath)
	if err != nil {
		log.Errorf("Rollback path error: %v", err)
		return
	}

	// Rollback backup config file to current
	err = backupAndSetLatest(backupConfigPath, currentConfigPath, "", "config", false, true)
	if err != nil {
		log.Errorf("Rollback error: %v", err)
		return
	}

	// Rollback backup btfs file to current
	err = backupAndSetLatest(btfsBackupPath, btfsBinaryPath, "", "btfs", true, true)
	if err != nil {
		log.Errorf("Rollback error: %v", err)
		return
	}

	// Rollback backup fs-repo-migrations file to current
	err = backupAndSetLatest(frmBackupPath, frmBinaryPath, "", "fs-repo-migrations", true, true)
	if err != nil {
		log.Errorf("Rollback error: %v", err)
		return
	}

	// Start the btfs daemon
	cmd := exec.Command(btfsBinaryPath, daemonArgs...)
	err = cmd.Start()
	// Check if the btfs daemon start success.
	if err != nil {
		log.Errorf("BTFS rollback failed, reasons: [%v]", err)
		return
	}

	log.Info("BTFS rollback SUCCESS!")
}

func backupAndSetLatest(curPath, backupPath, latestPath, filename string, exec, backup bool) error {
	// Delete backup file.
	if pathExists(backupPath) {
		err := os.Remove(backupPath)
		if err != nil {
			return fmt.Errorf("Delete backup %s file error, reasons: [%v]", filename, err)
		}
	} else if backup {
		return fmt.Errorf("Backup %s file must exist - rollback is impossible!", filename)
	}

	// Move current file to backup if exists.
	if pathExists(curPath) {
		err := os.Rename(curPath, backupPath)
		if err != nil {
			return fmt.Errorf("Move current %s file to backup error, reasons: [%v]", filename, err)
		}
	}

	// Move latest file (if exists) to current file.
	if latestPath != "" {
		err := os.Rename(latestPath, curPath)
		if err != nil {
			return fmt.Errorf("Move latest %s file to current error, reasons: [%v]\n", filename, err)
		}
	}

	if exec {
		// Add executable permissions to binary.
		err := os.Chmod(curPath, 0775)
		if err != nil {
			return fmt.Errorf("Chmod %s file to execute permissions error, reasons: [%v]\n", filename, err)
		}
	}

	return nil
}

// update performs the main update btfs/config/fs-repo-migrations routines.
func update() int {
	time.Sleep(time.Second * 5)
	defaultProjectPath := flag.String("project", "", "Specify project path.")
	defaultDownloadPath := flag.String("download", "", "Specify download path.")
	// Input Where your local node is running on, default value is localhost:5001.
	url := flag.String("url", "localhost:5001", "Node daemon's http server address.")
	defaultHval := flag.String("hval", "", "Specify H-value from BitTorrent Client.")

	flag.Parse()

	log.Info("BTFS auto update begin.")

	if *defaultProjectPath == "" || *defaultDownloadPath == "" {
		log.Error("Request param is nil.")
		return 1
	}

	// Select binary files and configure file path based on operating system.
	currentConfigPath, backupConfigPath, latestConfigPath,
		btfsBinaryPath, btfsBackupPath, latestBtfsBinaryPath,
		frmBinaryPath, frmBackupPath, latestFrmBinaryPath,
		err := getProjectPath(*defaultProjectPath, *defaultDownloadPath)
	if err != nil {
		log.Errorf("Update path error: %v", err)
		return 1
	}

	// Set latest config file to current
	err = backupAndSetLatest(currentConfigPath, backupConfigPath, latestConfigPath, "config", false, false)
	if err != nil {
		log.Errorf("Update error: %v", err)
		return 1
	}

	// Set latest btfs file to current
	err = backupAndSetLatest(btfsBinaryPath, btfsBackupPath, latestBtfsBinaryPath, "btfs", true, false)
	if err != nil {
		log.Errorf("Update error: %v", err)
		return 1
	}

	// Set latest fs-repo-migrations file to current
	err = backupAndSetLatest(frmBinaryPath, frmBackupPath, latestFrmBinaryPath, "fs-repo-migrations", true, false)
	if err != nil {
		log.Errorf("Update error: %v", err)
		return 1
	}

	daemonArgs := []string{"daemon"}
	if *defaultHval != "" {
		daemonArgs = append(daemonArgs, "--hval="+*defaultHval)
	}

	wg := &sync.WaitGroup{}
	wg.Add(1)
	go rollback(wg, *defaultProjectPath, *defaultDownloadPath, *url, daemonArgs)

	// Start the btfs daemon
	cmd := exec.Command(btfsBinaryPath, daemonArgs...)
	err = cmd.Start()
	if err != nil {
		log.Errorf("Error starting new BTFS process, reasons: [%v]", err)
		return 1
	}

	// Wait for the rollback program to complete.
	wg.Wait()

	log.Info("BTFS auto update SUCCESS!")
	return 0
}

func main() {
	os.Exit(update())
}

// Determine if the path file exists.
func pathExists(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	return false
}

// Select binary files and configure file path based on operating system.
func getProjectPath(defaultProjectPath, defaultDownloadPath string) (
	currentConfigPath, backupConfigPath, latestConfigPath,
	btfsBinaryPath, btfsBackupPath, latestBtfsBinaryPath,
	frmBinaryPath, frmBackupPath, latestFrmBinaryPath string,
	err error) {
	if (runtime.GOOS == "darwin" || runtime.GOOS == "linux" || runtime.GOOS == "windows") &&
		(runtime.GOARCH == "amd64" || runtime.GOARCH == "386" ||
			runtime.GOARCH == "arm64" || runtime.GOARCH == "arm") {
		ext := ""
		if runtime.GOOS == "windows" {
			ext = ".exe"
		}

		currentConfigPath = fmt.Sprint(defaultProjectPath, "config.yaml")
		backupConfigPath = fmt.Sprint(defaultDownloadPath, "config.yaml.bk")
		latestConfigPath = fmt.Sprint(defaultDownloadPath, fmt.Sprintf("config_%s_%s.yaml", runtime.GOOS, runtime.GOARCH))
		btfsBinaryPath = fmt.Sprint(defaultProjectPath, fmt.Sprintf("btfs%s", ext))
		btfsBackupPath = fmt.Sprint(defaultDownloadPath, fmt.Sprintf("btfs%s.bk", ext))
		latestBtfsBinaryPath = fmt.Sprint(defaultDownloadPath, fmt.Sprintf("btfs-%s-%s%s", runtime.GOOS, runtime.GOARCH, ext))
		frmBinaryPath = fmt.Sprint(defaultProjectPath, fmt.Sprintf("fs-repo-migrations%s", ext))
		frmBackupPath = fmt.Sprint(defaultDownloadPath, fmt.Sprintf("fs-repo-migrations%s.bk", ext))
		latestFrmBinaryPath = fmt.Sprint(defaultDownloadPath, fmt.Sprintf("fs-repo-migrations-%s-%s%s", runtime.GOOS, runtime.GOARCH, ext))
	} else {
		msg := fmt.Sprintf("Operating system [%s], arch [%s] does not support automatic updates",
			runtime.GOOS, runtime.GOARCH)
		err = fmt.Errorf(msg)
	}
	return
}
