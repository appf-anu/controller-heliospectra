package main

import (
	"bufio"
	"flag"
	"fmt"
	"github.com/bcampbell/fuzzytime"
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
	ctx        fuzzytime.Context
	zoneName   string
	zoneOffset int
)

var (
	noMetrics, dummy                           bool
	address                                    string
	multiplier                                 float64
	conditionsPath, hostTag, groupTag, userTag string
	interval                                   time.Duration
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
	the first 3 or 4 columns are used for running the chamber:
		date,time,temperature,humidity OR datetime,temperature,humidity
		the second case only occurs if the first 8 characters of the file (0th header) is "datetime"

	for the moment, the first line of the csv is technically (this is for your headers)
	if both -dummy and -no-metrics are specified, this program will exit.

`
	fmt.Printf(use, os.Args[0], os.Args[0], os.Args[0])
}

func parseDateTime(tString string) (time.Time, error) {

	datetimeValue, _, err := ctx.Extract(tString)
	if err != nil {
		errLog.Printf("couldn't extract datetime: %s", err)
	}

	datetimeValue.Time.SetHour(datetimeValue.Time.Hour())
	datetimeValue.Time.SetMinute(datetimeValue.Time.Minute())
	datetimeValue.Time.SetSecond(datetimeValue.Time.Second())
	datetimeValue.Time.SetTZOffset(zoneOffset)

	return time.Parse("2006-01-02T15:04:05Z07:00", datetimeValue.ISOFormat())
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
	for _, v := range tmpStrings[1:] {
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
	values = tmpStrings[1:]
	return
}

func runConditions() {
	errLog.Printf("running conditions file: %s\n", conditionsPath)
	file, err := os.Open(conditionsPath)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	idx := 0
	var lastTime time.Time
	var lastLineSplit []string
	firstRun := true
	for scanner.Scan() {
		line := scanner.Text()
		if idx == 0 {
			idx++
			continue
		}

		lineSplit := strings.Split(line, ",")
		timeStr := lineSplit[0]
		theTime, err := parseDateTime(timeStr)
		if err != nil {
			errLog.Println(err)
			continue
		}

		// if we are before the time skip until we are after it
		// the -10s means that we shouldnt run again.
		if theTime.Before(time.Now()) {
			lastLineSplit = lineSplit
			lastTime = theTime
			continue
		}

		if firstRun {
			firstRun = false
			errLog.Println("running firstrun line")
			for i := 0; i < 10; i++ {
				if runStuff(lastTime, lastLineSplit) {
					break
				}
			}
		}

		errLog.Printf("sleeping for %ds\n", int(time.Until(theTime).Seconds()))
		time.Sleep(time.Until(theTime))

		// RUN STUFF HERE
		for i := 0; i < 10; i++ {
			if runStuff(theTime, lineSplit) {
				break
			}
		}
		// end RUN STUFF
		idx++
	}
}

func min(x, y int) int {
	if x < y {
		return x
	}
	return y
}

func max(x, y int) int {
	if x > y {
		return x
	}
	return y
}

func minMax(value int) int {
	return min(max(value, 0), 1000)
}

func intToString(a []int) []string {
	b := make([]string, len(a))
	for i, v := range a {
		b[i] = strconv.Itoa(v)
	}
	return b
}

func setMany(conn *telnet.Conn, values []float64) (err error) {
	intVals := make([]int, len(values))
	for i,x := range values{
		intVals[i] = minMax(int(x*multiplier))
	}
	command := "setWlsRelPower "
	command += strings.Join(intToString(intVals), " ")

	_, err = execCommand(conn, command)
	return
}

func getPower(conn *telnet.Conn) (values []float64, err error) {
	intValues, err := chompAllInts(conn, "getAllRelPower")
	for _, v := range intValues{
		values = append(values, float64(v)/multiplier)
	}
	return
}

func getWl(conn *telnet.Conn) (values []string, err error) {
	values, err = chompAllStrings(conn, "getWl")
	return
}

// runStuff, should send values and write metrics.
// returns true if program should continue, false if program should retry
func runStuff(theTime time.Time, lineSplit []string) bool {
	stringVals := lineSplit[4:]
	lightValues := make([]float64, len(stringVals))

	for i, v := range stringVals {
		found := matchFloat.FindString(v)
		if len(found) < 0 {
			errLog.Printf("couldnt parse %s as float.\n", v)
			continue
		}
		fl, err := strconv.ParseFloat(found, 64)
		if err != nil {
			errLog.Println(err)
			continue
		}
		lightValues[i] = fl
	}
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
	minLength := min(len(wavelengths),len(lightValues))
	if len(lightValues) != minLength{
		errLog.Println("Different number of light values than wavelengths")
	}

	err = setMany(conn, lightValues[:minLength])
	if err != nil{
		errLog.Println(err)
		return false
	}

	errLog.Println("ran ", theTime.Format("2006-01-02T15:04:05"), lightValues)

	for x := 0; x < 5; x++ {
		if err := writeMetrics(wavelengths, lightValues); err != nil {
			errLog.Println(err)
			time.Sleep(200 * time.Millisecond)
			continue
		}
		break
	}
	return true
}

func writeMetrics(wavelengths []string, lightValues []float64) error {
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

		m := telegraf.NewMeasurement("helio-light")
		if len(wavelengths) != len(lightValues) {
			return fmt.Errorf("wavelengths and light values differ")
		}

		for i, v := range lightValues {
			wl,err := strconv.ParseInt(wavelengths[i], 10, 64)
			if err != nil{
				errLog.Println(err)
				continue
			}
			if wl == 6500 {
				m.AddFloat64(fmt.Sprintf("%dk", wl), v)
				continue
			}
			m.AddFloat64(fmt.Sprintf("%dnm", wl), v)
		}
		if hostTag != "" {
			m.AddTag("host", hostTag)
		}
		if groupTag != "" {
			m.AddTag("group", groupTag)
		}
		if userTag != "" {
			m.AddTag("user", userTag)
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
	zoneName, zoneOffset = time.Now().Zone()

	ctx = fuzzytime.Context{
		DateResolver: fuzzytime.DMYResolver,
		TZResolver:   fuzzytime.DefaultTZResolver(zoneName),
	}
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

	flag.StringVar(&hostTag, "host-tag", hostname, "host tag to add to the measurements")
	if tempV := os.Getenv("HOST_TAG"); tempV != "" {
		hostTag = tempV
	}

	flag.StringVar(&groupTag, "group-tag", "nonspc", "host tag to add to the measurements")
	if tempV := os.Getenv("GROUP_TAG"); tempV != "" {
		groupTag = tempV
	}

	flag.StringVar(&userTag, "user-tag", "", "user specified tag")
	if tempV := os.Getenv("USER_TAG"); tempV != "" {
		userTag = tempV
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

	errLog.Printf("timezone: \t%s\n", zoneName)
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
			if err != nil{
				errLog.Println(err)
				return
			}
			lightWavelengths, err := getWl(conn)
			if err != nil{
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
		runConditions()
	}

}
