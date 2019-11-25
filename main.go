package main

import (
	"github.com/appf-anu/chamber-tools"
	"flag"
	"fmt"
	"github.com/mdaffin/go-telegraf"
	"github.com/ziutek/telnet"
	"log"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	errLog     *log.Logger
)

var (
	noMetrics, dummy, loopFirstDay            bool
	address                                   string
	multiplier                                float64
	conditionsPath, hostTag, groupTag, didTag string
	interval                                  time.Duration
)

const (
	matchFloatExp   = `[-+]?\d*\.\d+|\d+`
	matchIntsExp    = `\b(\d+)\b`
	matchOKExp      = `OK`
	matchStringsExp = `\b(\w+)\b`
)

// TsRegex is a regexp to find a timestamp within a filename
var /* const */ matchFloat = regexp.MustCompile(matchFloatExp)
var /* const */ matchInts = regexp.MustCompile(matchIntsExp)
var /* const */ matchOK = regexp.MustCompile(matchOKExp)
var /* const */ matchStrings = regexp.MustCompile(matchStringsExp)

const (
	// it is extremely unlikely (see. impossible) that we will be measuring a humidity of 214,748,365 %RH or a
	// temperature of -340,282,346,638,528,859,811,704,183,484,516,925,440°C until we invent some new physics, so until
	// then, I will use these values as the unset or null values for HumidityTarget and TemperatureTarget
	nullTargetInt   = math.MinInt32
	nullTargetFloat = -math.MaxFloat32
)

var usage = func() {
	use := `
usage of %s:
flags:
	-no-metrics: don't send metrics to telegraf
	-dummy: don't control the chamber, only collect metrics (this is implied by not specifying a conditions file
	-conditions: conditions to use to run the chamber
	-interval: what interval to run conditions/record metrics at, set to 0s to read 1 metric and exit. (default=10m)

examples:
	collect data on 192.168.1.3  and output the errors to GC03-error.log and record the output to GC03.log
	%s -dummy 192.168.1.3 2>> GC03-error.log 1>> GC03.log

	run conditions on 192.168.1.3  and output the errors to GC03-error.log and record the output to GC03.log
	%s -conditions GC03-conditions.csv -dummy 192.168.1.3 2>> GC03-error.log 1>> GC03.log

quirks:
channels are sequentially numbered as such in conditions file:

	s7:
		channel-1 400nm
		channel-2 420nm
		channel-3 450nm
		channel-4 530nm
		channel-5 630nm
		channel-6 660nm
		channel-7 735nm
	s10:
		channel-1 370nm
		channel-2 400nm
		channel-3 420nm
		channel-4 450nm
		channel-5 530nm
		channel-6 620nm
		channel-7 660nm
		channel-8 735nm
		channel-9 850nm
		channel-10 6500k
`
	fmt.Printf(use, os.Args[0], os.Args[0], os.Args[0])
}


func execCommand(conn *telnet.Conn, command string) (ret string, err error) {
	// write command
	conn.Write([]byte(command + "\n"))
	// read 1 newline cos this is ours.
	datad, err := conn.ReadString('>')

	if err != nil {
		return
	}

	if matchOK.MatchString(datad) != true {
		err = fmt.Errorf(strings.TrimSpace(string(datad)))
		return
	}

	// trim...
	ret = strings.TrimSpace(string(datad))
	return
}

func chompAllInts(conn *telnet.Conn, command string) (values []int, err error) {
	data, err := execCommand(conn, command)
	if err != nil {
		return
	}

	// find the ints
	tmpStrings := matchInts.FindAllString(data, -1)
	for _, v := range tmpStrings {
		i, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return values, err
		}
		values = append(values, int(i))
	}

	return
}

func chompAllStrings(conn *telnet.Conn, command string) (values []string, err error) {
	data, err := execCommand(conn, command)
	if err != nil {
		return
	}

	// find the ints
	tmpStrings := matchStrings.FindAllString(data, -1)
	values = tmpStrings[1:] // discard first string coz "OK"
	return
}

func intToString(a []int) []string {
	b := make([]string, len(a))
	for i, v := range a {
		b[i] = strconv.Itoa(v)
	}
	return b
}


func setMany(conn *telnet.Conn, values []int) (err error) {
	stringWls := strings.Join(intToString(values), " ")
	command := fmt.Sprintf("setWlsRelPower %s", stringWls)
	_, err = execCommand(conn, command)
	return
}

func setOne(conn *telnet.Conn, wl int, value int) (err error){
	command := fmt.Sprintf("setWlRelPower %d %d", wl, value)
	_, err = execCommand(conn, command)
	return
}

func getPower(conn *telnet.Conn) (values []int, err error) {
	values, err = chompAllInts(conn, "getAllRelPower")
	return
}

func getWl(conn *telnet.Conn) (values []string, err error) {
	values, err = chompAllStrings(conn, "getWl")
	return
}

// runStuff, should send values and write metrics.
// returns true if program should continue, false if program should retry
func runStuff(point *chamber_tools.TimePoint) bool {

	conn, err := telnet.DialTimeout("tcp", address, time.Second*30)
	if err != nil {
		errLog.Println(err)
		return false
	}
	defer conn.Close()
	err = conn.SkipUntil("\n>")
	if err != nil {
		errLog.Println(err)
		return false
	}

	wavelengths, err := getWl(conn)
	if err != nil {
		errLog.Println(err)
		return false
	}
	minLength := chamber_tools.Min(len(wavelengths), len(point.Channels))
	if len(point.Channels) < len(wavelengths){
		errLog.Printf("Number of light values in control file (%d) less than wavelengths/channels for this " +
			"light (%d), ignoring some channels.\n", len(point.Channels), len(wavelengths))
	}
	if len(point.Channels) > len(wavelengths) {
		errLog.Printf("Number of light values in control file (%d) greater than wavelengths/channels for " +
			"this light (%d), ignoring some channels.\n", len(point.Channels), len(wavelengths))
	}

	// make intvals the minimum length
	intVals := make([]int, minLength)
	negVal := false
	// iterate over the minimum length
	for i, _ := range intVals {
		// multiply all the channel values by the multiplier.
		// none of the heliospectras accept values over 1000, so clamp
		intVals[i] = chamber_tools.Clamp(int(point.Channels[i] * multiplier), 0, 1000)
		if point.Channels[i] < 0 {
			negVal = true
		}
	}
	// handle negative / non-provided values
	if negVal {
		for i, value := range intVals {
			// skip negative values
			if value < 0 {
				continue
			}

			// get the wavelength as an int
			wlInt, err := strconv.Atoi(strings.TrimSpace(wavelengths[i])) // get the wavelength as an int
			if err != nil {
				errLog.Printf("error converting wavelength value %s to int to set value %d\n",
					wavelengths[i], value)
				errLog.Println(err)
				continue
			}

			// set the value
			err = setOne(conn, wlInt, value)
			if err != nil {
				errLog.Printf("Couldn't set wl %s to %d\n", wlInt, value)
				errLog.Println(err)
				continue
			}
		}
	} else {
		err = setMany(conn, intVals)
		if err != nil {
			errLog.Println(err)
			return false
		}
	}

	errLog.Println("scaling ", multiplier)
	errLog.Println("ran ", point.Datetime.Format("2006-01-02T15:04:05"), intVals)

	time.Sleep(time.Millisecond * 50)
	returnedLv, err := getPower(conn)
	if err != nil {
		errLog.Println(err)
		return false
	}
	errLog.Println("got ", point.Datetime.Format("2006-01-02T15:04:05"), returnedLv)

	for x := 0; x < 5; x++ {
		if err := writeMetrics(wavelengths, returnedLv); err != nil {
			errLog.Println(err)
			time.Sleep(200 * time.Millisecond)
			continue
		}
		break
	}
	return true
}

func writeMetrics(wavelengths []string, lightValues []int) error {
	if !noMetrics {
		telegrafHost := "telegraf:8092"
		if os.Getenv("TELEGRAF_HOST") != "" {
			telegrafHost = os.Getenv("TELEGRAF_HOST")
		}

		telegrafClient, err := telegraf.NewUDP(telegrafHost)
		if err != nil {
			return err
		}
		defer telegrafClient.Close()

		m := telegraf.NewMeasurement("heliospectra-light")
		if len(wavelengths) != len(lightValues) {
			return fmt.Errorf("wavelengths and light values differ")
		}

		for i, v := range lightValues {
			wl, err := strconv.ParseInt(wavelengths[i], 10, 64)
			if err != nil {
				errLog.Println(err)
				continue
			}
			if wl == 6500 {
				m.AddInt(fmt.Sprintf("%dk", wl), v)
				continue
			}
			m.AddInt(fmt.Sprintf("%dnm", wl), v)
		}
		if hostTag != "" {
			m.AddTag("host", hostTag)
		}
		if groupTag != "" {
			m.AddTag("group", groupTag)
		}
		if didTag != "" {
			m.AddTag("did", didTag)
		}

		telegrafClient.Write(m)
	}
	return nil
}

func init() {
	var err error
	hostname := os.Getenv("NAME")

	if address = os.Getenv("ADDRESS"); address == "" {
		address = flag.Arg(0)
		if err != nil {
			panic(err)
		}
	}

	errLog = log.New(os.Stderr, "[heliospectra] ", log.Ldate|log.Ltime|log.Lshortfile)
	// get the local zone and offset

	flag.Usage = usage
	flag.BoolVar(&noMetrics, "no-metrics", false, "dont collect metrics")
	if tempV := strings.ToLower(os.Getenv("NO_METRICS")); tempV != "" {
		if tempV == "true" || tempV == "1" {
			noMetrics = true
		} else {
			noMetrics = false
		}
	}

	flag.BoolVar(&dummy, "dummy", false, "dont send conditions to light")
	if tempV := strings.ToLower(os.Getenv("DUMMY")); tempV != "" {
		if tempV == "true" || tempV == "1" {
			dummy = true
		} else {
			dummy = false
		}
	}

	flag.BoolVar(&loopFirstDay, "loop", false, "loop over the first day")
	if tempV := strings.ToLower(os.Getenv("LOOP")); tempV != "" {
		if tempV == "true" || tempV == "1" {
			loopFirstDay = true
		} else {
			loopFirstDay = false
		}
	}

	flag.StringVar(&hostTag, "host-tag", hostname, "host tag to add to the measurements")
	if tempV := os.Getenv("HOST_TAG"); tempV != "" {
		hostTag = tempV
	}

	flag.StringVar(&groupTag, "group-tag", "nonspc", "host tag to add to the measurements")
	if tempV := os.Getenv("GROUP_TAG"); tempV != "" {
		groupTag = tempV
	}

	flag.StringVar(&didTag, "did-tag", "", "deliverable id tag")
	if tempV := os.Getenv("DID_TAG"); tempV != "" {
		didTag = tempV
	}

	flag.StringVar(&conditionsPath, "conditions", "", "conditions file to")
	if tempV := os.Getenv("CONDITIONS_FILE"); tempV != "" {
		conditionsPath = tempV
	}
	flag.DurationVar(&interval, "interval", time.Minute*10, "interval to run conditions/record metrics at")
	if tempV := os.Getenv("INTERVAL"); tempV != "" {
		interval, err = time.ParseDuration(tempV)
		if err != nil {
			errLog.Println("Couldnt parse interval from environment")
			errLog.Println(err)
		}
	}
	flag.Float64Var(&multiplier, "multiplier", 10.0, "multiplier for the light")
	if tempV := os.Getenv("MULTIPLIER"); tempV != "" {
		multiplier, err = strconv.ParseFloat(tempV, 64)
		if err != nil {
			errLog.Println("Couldnt parse multiplier from environment")
			errLog.Println(err)
		}
	}
	flag.Parse()

	if noMetrics && dummy {
		errLog.Println("dummy and no-metrics specified, nothing to do.")
		os.Exit(1)
	}
	if conditionsPath != "" && !dummy {
		chamber_tools.InitIndexConfig(errLog, conditionsPath)
	}
	errLog.Printf("hostTag: \t%s\n", hostTag)
	errLog.Printf("groupTag: \t%s\n", groupTag)
	errLog.Printf("address: \t%s\n", address)
	errLog.Printf("file: \t%s\n", conditionsPath)
	errLog.Printf("interval: \t%s\n", interval)
}

func main() {
	if !noMetrics && (conditionsPath == "" || dummy) {

		runMetrics := func() {
			conn, err := telnet.DialTimeout("tcp", address, time.Second*30)
			if err != nil {
				errLog.Println(err)
			}
			defer conn.Close()
			err = conn.SkipUntil(">")
			if err != nil {
				errLog.Println(err)
				return
			}

			lightPower, err := getPower(conn)
			if err != nil {
				errLog.Println(err)
				return
			}
			lightWavelengths, err := getWl(conn)
			if err != nil {
				errLog.Println(err)
				return
			}
			writeMetrics(lightWavelengths, lightPower)

			fmt.Println("wavelengths:\t\t", lightWavelengths)
			fmt.Println("power:\t\t", lightPower)
		}

		runMetrics()

		ticker := time.NewTicker(interval)
		go func() {
			for range ticker.C {
				runMetrics()
			}
		}()
		select {}
	}

	if conditionsPath != "" && !dummy {
		chamber_tools.RunConditions(errLog, runStuff, conditionsPath, loopFirstDay)
	}

}
