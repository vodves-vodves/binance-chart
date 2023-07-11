package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/components"
	"github.com/go-echarts/go-echarts/v2/opts"
	"github.com/imroc/req/v3"
	"github.com/tidwall/gjson"
	"io/fs"
	"log"
	"net/http"
	"os"
	"time"
)

var (
	client   = req.C()
	allCoins []string
)

type jsonStruct struct {
	//CalcTime    int64     `json:"calcTime"`
	TotalBorrow []float64 `json:"totalBorrow"`
	TotalRepay  []float64 `json:"totalRepay"`
	TotalTime   []int64   `json:"totalTime"`
}

func main() {
	defer func() {
		if err := recover(); err != nil {
			log.Printf("panic: %v\n", err)
			reader := bufio.NewReader(os.Stdin)
			log.Println("Press ENTER to exit")
			_, _ = reader.ReadString('\n')
		}
	}()

	start()
}

func start() {
	var err error
	allCoins, err = getAllCoins()
	if err != nil {
		log.Println("err:", err)
		return
	}

	path := "coinsJson"
	err = os.MkdirAll(path, os.ModePerm)
	if err != nil {
		log.Println("err:", err)
		return
	}

	for _, coin := range allCoins {
		_, err := readJson(coin)
		if err != nil {
			log.Println("err:", err)
			return
		}
	}

	go func() {
		work()
	}()

	http.HandleFunc("/", drawCharts)
	log.Println("running server at http://localhost:8081")
	log.Fatal(http.ListenAndServe(":8081", nil))
}

func work() {
	var (
		totalBorrowS = make([]float64, 0)
		totalRepayS  = make([]float64, 0)
		totalTimeS   = make([]int64, 0)
	)
	url := "https://www.binance.com/bapi/margin/v1/public/margin/statistics/24h-borrow-and-repay"

	oldD := time.Now().Unix()

	for {
		func() {
			defer time.Sleep(60 * time.Second)

			result, err := sendRequest(url)
			if err != nil {
				log.Println("err:", err)
				return
			}

			calcTimer := gjson.Get(result, "data.calculationTime").Int()

			if calcTimer > oldD {
				coins := gjson.Get(result, "data.coins")
				for _, coin := range coins.Array() {
					//if asset := coin.Get("asset").String(); asset == needCoin {
					coinName := coin.Get("asset").String()
					data, err := readJson(coinName)
					if err != nil {
						log.Println("err:", err)
						continue
					}
					totalBorrowS = data.TotalBorrow
					totalRepayS = data.TotalRepay
					totalTimeS = data.TotalTime

					totalBorrowS = append(totalBorrowS, coin.Get("totalBorrowInUsdt").Float())
					totalRepayS = append(totalRepayS, coin.Get("totalRepayInUsdt").Float())
					totalTimeS = append(totalTimeS, time.Now().Unix())

					saveJson := jsonStruct{
						//CalcTime:    calcTimer,
						TotalBorrow: totalBorrowS,
						TotalRepay:  totalRepayS,
						TotalTime:   totalTimeS,
					}
					err = writeJson(saveJson, coinName)
					if err != nil {
						log.Println("err:", err)
						continue
					}

				}
				oldD = calcTimer
				log.Println("ok")
			} else {
				log.Println("waiting")
			}
		}()
	}
}

func sendRequest(url string) (string, error) {
	resp, err := client.R().
		Get(url)
	if err != nil {
		return "", err
	}
	return resp.String(), nil
}

func readJson(coin string) (jsonStruct, error) {
	jsonF := jsonStruct{}
	fileName := fmt.Sprintf("coinsJson/%s.json", coin)
	file, err := os.ReadFile(fileName)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			file, err := os.Create(fileName)
			if err != nil {
				return jsonStruct{}, err
			}
			content, err := json.MarshalIndent(jsonF, "", "\t")
			if err != nil {
				return jsonStruct{}, err
			}
			file.Write(content)
			file.Close()
		}
	} else {
		if err := json.Unmarshal(file, &jsonF); err != nil {
			return jsonStruct{}, err
		}
		return jsonF, nil
	}
	file, _ = os.ReadFile(fileName)
	if err := json.Unmarshal(file, &jsonF); err != nil {
		return jsonStruct{}, err
	}
	return jsonF, nil
}

func writeJson(saveJson jsonStruct, coin string) error {
	fileName := fmt.Sprintf("coinsJson/%s.json", coin)
	file, err := os.OpenFile(fileName, os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	content, err := json.MarshalIndent(saveJson, "", "\t")
	if err != nil {
		return err
	}
	_, err = file.Write(content)
	if err != nil {
		return err
	}
	return nil
}

func drawCharts(w http.ResponseWriter, _ *http.Request) {
	page := components.NewPage()
	for _, coinName := range allCoins {
		chart, err := generateChart(coinName)
		if err != nil {
			log.Println("err:", err)
			continue
		}
		page.AddCharts(
			chart,
		)
	}
	page.Render(w)
}

func generateChart(coinName string) (*charts.Line, error) {
	items1, items2, times, err := getLineItems(coinName)
	if err != nil {
		return nil, err
	}

	line := charts.NewLine()

	line.SetGlobalOptions(
		//charts.WithInitializationOpts(opts.Initialization{Theme: types.ThemeWalden}),
		charts.WithTitleOpts(opts.Title{
			Title: fmt.Sprintf("График %s", coinName),
		}),
		//charts.WithTooltipOpts(opts.Tooltip{Show: true, Trigger: "axis"}),
		charts.WithYAxisOpts(opts.YAxis{
			Scale: true,
		}),
		charts.WithDataZoomOpts(opts.DataZoom{
			Start:      0,
			End:        100,
			XAxisIndex: []int{0},
		}),
	)

	line.SetXAxis(times).
		AddSeries("Borrowed", items1).
		AddSeries("Payed", items2).
		SetSeriesOptions(charts.WithLineChartOpts(
			opts.LineChart{Smooth: true, ShowSymbol: true, SymbolSize: 10, Symbol: "circle"},
		),
			charts.WithLabelOpts(opts.Label{
				Show: true,
			}))

	return line, nil
}

func getLineItems(coinName string) ([]opts.LineData, []opts.LineData, []string, error) {
	items1 := make([]opts.LineData, 0)
	items2 := make([]opts.LineData, 0)
	times := make([]string, 0)
	data, err := readJson(coinName)
	if err != nil {
		return []opts.LineData{}, []opts.LineData{}, []string{}, err
	}
	for _, value := range data.TotalBorrow {
		items1 = append(items1, opts.LineData{Value: value})
	}
	for _, value := range data.TotalRepay {
		items2 = append(items2, opts.LineData{Value: value})
	}
	for _, value := range data.TotalTime {
		times = append(times, time.Unix(value, 0).Format("02-01-2006 15:04:05"))
	}

	return items1, items2, times, nil
}

func getAllCoins() ([]string, error) {
	coinNames := make([]string, 0)
	url := "https://www.binance.com/bapi/margin/v1/public/margin/statistics/24h-borrow-and-repay"
	result, err := sendRequest(url)
	if err != nil {
		return nil, err
	}
	coins := gjson.Get(result, "data.coins").Array()
	for _, coin := range coins {
		coinNames = append(coinNames, coin.Get("asset").String())
	}
	return coinNames, nil
}
