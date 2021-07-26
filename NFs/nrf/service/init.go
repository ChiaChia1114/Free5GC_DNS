package service

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/go-redis/redis"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli"

	"github.com/free5gc/MongoDBLibrary"
	mongoDBLibLogger "github.com/free5gc/MongoDBLibrary/logger"
	"github.com/free5gc/http2_util"
	"github.com/free5gc/logger_util"
	"github.com/free5gc/nrf/accesstoken"
	nrf_context "github.com/free5gc/nrf/context"
	"github.com/free5gc/nrf/discovery"
	"github.com/free5gc/nrf/factory"
	"github.com/free5gc/nrf/logger"
	"github.com/free5gc/nrf/management"
	"github.com/free5gc/nrf/util"
	openApiLogger "github.com/free5gc/openapi/logger"
	"github.com/free5gc/path_util"
	pathUtilLogger "github.com/free5gc/path_util/logger"
)

type NRF struct{}

type (
	// Config information.
	Config struct {
		nrfcfg string
	}
)

var config Config

var nrfCLi = []cli.Flag{
	cli.StringFlag{
		Name:  "free5gccfg",
		Usage: "common config file",
	},
	cli.StringFlag{
		Name:  "nrfcfg",
		Usage: "config file",
	},
}

var initLog *logrus.Entry

func init() {
	initLog = logger.InitLog
}

func (*NRF) GetCliCmd() (flags []cli.Flag) {
	return nrfCLi
}

func (nrf *NRF) Initialize(c *cli.Context) error {
	config = Config{
		nrfcfg: c.String("nrfcfg"),
	}

	if config.nrfcfg != "" {
		if err := factory.InitConfigFactory(config.nrfcfg); err != nil {
			return err
		}
	} else {
		DefaultNrfConfigPath := path_util.Free5gcPath("free5gc/config/nrfcfg.yaml")
		if err := factory.InitConfigFactory(DefaultNrfConfigPath); err != nil {
			return err
		}
	}

	nrf.setLogLevel()

	if err := factory.CheckConfigVersion(); err != nil {
		return err
	}

	return nil
}

func (nrf *NRF) setLogLevel() {
	if factory.NrfConfig.Logger == nil {
		initLog.Warnln("NRF config without log level setting!!!")
		return
	}

	if factory.NrfConfig.Logger.NRF != nil {
		if factory.NrfConfig.Logger.NRF.DebugLevel != "" {
			level, err := logrus.ParseLevel(factory.NrfConfig.Logger.NRF.DebugLevel)
			if err != nil {
				initLog.Warnf("NRF Log level [%s] is invalid, set to [info] level",
					factory.NrfConfig.Logger.NRF.DebugLevel)
				logger.SetLogLevel(logrus.InfoLevel)
			} else {
				initLog.Infof("NRF Log level is set to [%s] level", level)
				logger.SetLogLevel(level)
			}
		} else {
			initLog.Infoln("NRF Log level not set. Default set to [info] level")
			logger.SetLogLevel(logrus.InfoLevel)
		}
		logger.SetReportCaller(factory.NrfConfig.Logger.NRF.ReportCaller)
	}

	if factory.NrfConfig.Logger.PathUtil != nil {
		if factory.NrfConfig.Logger.PathUtil.DebugLevel != "" {
			if level, err := logrus.ParseLevel(factory.NrfConfig.Logger.PathUtil.DebugLevel); err != nil {
				pathUtilLogger.PathLog.Warnf("PathUtil Log level [%s] is invalid, set to [info] level",
					factory.NrfConfig.Logger.PathUtil.DebugLevel)
				pathUtilLogger.SetLogLevel(logrus.InfoLevel)
			} else {
				pathUtilLogger.SetLogLevel(level)
			}
		} else {
			pathUtilLogger.PathLog.Warnln("PathUtil Log level not set. Default set to [info] level")
			pathUtilLogger.SetLogLevel(logrus.InfoLevel)
		}
		pathUtilLogger.SetReportCaller(factory.NrfConfig.Logger.PathUtil.ReportCaller)
	}

	if factory.NrfConfig.Logger.OpenApi != nil {
		if factory.NrfConfig.Logger.OpenApi.DebugLevel != "" {
			if level, err := logrus.ParseLevel(factory.NrfConfig.Logger.OpenApi.DebugLevel); err != nil {
				openApiLogger.OpenApiLog.Warnf("OpenAPI Log level [%s] is invalid, set to [info] level",
					factory.NrfConfig.Logger.OpenApi.DebugLevel)
				openApiLogger.SetLogLevel(logrus.InfoLevel)
			} else {
				openApiLogger.SetLogLevel(level)
			}
		} else {
			openApiLogger.OpenApiLog.Warnln("OpenAPI Log level not set. Default set to [info] level")
			openApiLogger.SetLogLevel(logrus.InfoLevel)
		}
		openApiLogger.SetReportCaller(factory.NrfConfig.Logger.OpenApi.ReportCaller)
	}

	if factory.NrfConfig.Logger.MongoDBLibrary != nil {
		if factory.NrfConfig.Logger.MongoDBLibrary.DebugLevel != "" {
			if level, err := logrus.ParseLevel(factory.NrfConfig.Logger.MongoDBLibrary.DebugLevel); err != nil {
				mongoDBLibLogger.MongoDBLog.Warnf("MongoDBLibrary Log level [%s] is invalid, set to [info] level",
					factory.NrfConfig.Logger.MongoDBLibrary.DebugLevel)
				mongoDBLibLogger.SetLogLevel(logrus.InfoLevel)
			} else {
				mongoDBLibLogger.SetLogLevel(level)
			}
		} else {
			mongoDBLibLogger.MongoDBLog.Warnln("MongoDBLibrary Log level not set. Default set to [info] level")
			mongoDBLibLogger.SetLogLevel(logrus.InfoLevel)
		}
		mongoDBLibLogger.SetReportCaller(factory.NrfConfig.Logger.MongoDBLibrary.ReportCaller)
	}
}

func (nrf *NRF) FilterCli(c *cli.Context) (args []string) {
	for _, flag := range nrf.GetCliCmd() {
		name := flag.GetName()
		value := fmt.Sprint(c.Generic(name))
		if value == "" {
			continue
		}

		args = append(args, "--"+name, value)
	}
	return args
}

func (nrf *NRF) Start() {
	MongoDBLibrary.SetMongoDB(factory.NrfConfig.Configuration.MongoDBName, factory.NrfConfig.Configuration.MongoDBUrl)
	initLog.Infoln("Server started")

	router := logger_util.NewGinWithLogrus(logger.GinLog)

	accesstoken.AddService(router)
	discovery.AddService(router)
	management.AddService(router)

	nrf_context.InitNrfContext()

	signalChannel := make(chan os.Signal, 1)
	signal.Notify(signalChannel, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signalChannel
		// Waiting for other NFs to deregister
		time.Sleep(2 * time.Second)
		nrf.Terminate()
		os.Exit(0)
	}()

	bindAddr := factory.NrfConfig.GetSbiBindingAddr()
	initLog.Infof("Binding addr: [%s]", bindAddr)
	//--------------For Redis start --------------------//

	client := redis.NewClient(&redis.Options{
		Addr: "127.0.0.1:6379",
		//Password: "123456",
		DB: 0,
	})

	val, err := client.Get("golang").Result()
	if err != nil {
		fmt.Println(err)
	}

	time1, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		fmt.Println(err)
	}
	time2 := time.Now().UnixNano() / 1e3
	fmt.Println("NRF config loading end time is :", time2)
	time_total := time2 - time1

	fmt.Println("Latency for NRF loading is : ", time_total)

	pong, err := client.Ping().Result()
	fmt.Println(pong, err)

	client.LPush("nrflatency", time_total)
	fmt.Println("time_total stored")

	count, err := client.LLen("nrflatency").Result()
	if err != nil {
		panic(err)
	}
	fmt.Println(count)
	if count >= 10 {
		fmt.Println("Process of printing average latency")

		var nrf [11]string
		var nrf_int [11]int64
		var latency_total int64
		var i int64
		i = 1
		latency_total = 0
		for i <= count {
			nrf[i], err = client.LPop("nrflatency").Result()
			if err != nil {
				panic(err)
			}
			nrf_int[i], err = strconv.ParseInt(nrf[i], 10, 64)
			if err != nil {
				panic(err)
			}
			latency_total = latency_total + nrf_int[i]
			i++
		}
		var latency_avg int64
		count = int64(count)
		latency_avg = 0
		latency_avg = latency_total / count
		client.LPush("nrflatency", latency_avg)
		fmt.Println("latency average is stored")
	}

	//-------------For Redis End -----------------------//
	server, err := http2_util.NewServer(bindAddr, util.NrfLogPath, router)

	if server == nil {
		initLog.Errorf("Initialize HTTP server failed: %+v", err)
		return
	}

	if err != nil {
		initLog.Warnf("Initialize HTTP server: +%v", err)
	}

	serverScheme := factory.NrfConfig.GetSbiScheme()
	if serverScheme == "http" {
		err = server.ListenAndServe()
	} else if serverScheme == "https" {
		err = server.ListenAndServeTLS(util.NrfPemPath, util.NrfKeyPath)
	}

	if err != nil {
		initLog.Fatalf("HTTP server setup failed: %+v", err)
	}

}

func (nrf *NRF) Exec(c *cli.Context) error {
	initLog.Traceln("args:", c.String("nrfcfg"))
	args := nrf.FilterCli(c)
	initLog.Traceln("filter: ", args)
	command := exec.Command("./nrf", args...)

	if err := nrf.Initialize(c); err != nil {
		return err
	}

	stdout, err := command.StdoutPipe()
	if err != nil {
		initLog.Fatalln(err)
	}
	wg := sync.WaitGroup{}
	wg.Add(3)
	go func() {
		in := bufio.NewScanner(stdout)
		for in.Scan() {
			fmt.Println(in.Text())
		}
		wg.Done()
	}()

	stderr, err := command.StderrPipe()
	if err != nil {
		initLog.Fatalln(err)
	}
	go func() {
		in := bufio.NewScanner(stderr)
		fmt.Println("NRF log start")
		for in.Scan() {
			fmt.Println(in.Text())
		}
		wg.Done()
	}()

	go func() {
		fmt.Println("NRF  start")
		if err = command.Start(); err != nil {
			fmt.Printf("NRF Start error: %v", err)
		}
		fmt.Println("NRF  end")
		wg.Done()
	}()

	wg.Wait()

	return err
}

func (nrf *NRF) Terminate() {
	logger.InitLog.Infof("Terminating NRF...")

	logger.InitLog.Infof("NRF terminated")
}
