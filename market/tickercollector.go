/*

  Copyright 2017 Loopring Project Ltd (Loopring Foundation).

  Licensed under the Apache License, Version 2.0 (the "License");
  you may not use this file except in compliance with the License.
  You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

  Unless required by applicable law or agreed to in writing, software
  distributed under the License is distributed on an "AS IS" BASIS,
  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
  See the License for the specific language governing permissions and
  limitations under the License.

*/

package market

import (
	"encoding/json"
	"fmt"
	"github.com/Loopring/relay/cache"
	"github.com/robfig/cron"
	"io/ioutil"
	"net/http"
	"strings"
	"github.com/Loopring/relay/market/util"
	"github.com/Loopring/relay/log"
	"qiniupkg.com/x/errors.v7"
	"strconv"
)

type ExchangeType string

const (
	Binance ExchangeType = "binance"
	OkEx    ExchangeType = "okex"
	Huobi   ExchangeType = "huobi"
)

const cachePreKey = "TICKER_EX_"

//TODO (xiaolu)  add more exchanges to this list
var exchanges = map[string]string{
	"binance": "https://api.binance.com/api/v1/ticker/24hr?symbol=%s",
	"okex":    "https://www.okex.com/api/v1/ticker.do?symbol=%s",
	"huobi":   "https://api.huobi.pro/market/detail/merged?symbol=%s",
}

const defaultSyncInterval = 5 // minutes

type Exchange interface {
	updateCache()
}

type ExchangeImpl struct {
	name      string
	tickerUrl string
}

type Collector interface {
	getTickers(market string) ([]Ticker, error)
	Start()
}

type CollectorImpl struct {
	exs          []ExchangeImpl
	syncInterval int
	cron         *cron.Cron
}

func NewExchange(name, tickerUrl string) ExchangeImpl {
	return ExchangeImpl{name, tickerUrl}
}

func (e *ExchangeImpl) updateCache() {
	log.Info("step in update cache method........")

	switch e.name {
	case "binance":
		updateCacheByExchange(e.name, GetTickerFromBinance)
	case "okex":
		updateCacheByExchange(e.name, GetTickerFromOkex)
	case "huobi":
		updateCacheByExchange(e.name, GetTickerFromHuobi)
	}
}

func updateBinanceCache() {
	updateCacheByExchange("binance", GetTickerFromBinance)
}
func updateOkexCache() {
	updateCacheByExchange("okex", GetTickerFromOkex)
}
func updateHuobiCache() {
	updateCacheByExchange("huobi", GetTickerFromHuobi)
}

func updateCacheByExchange(exchange string, getter func(mkt string) (ticker Ticker, err error))  {
	for _, v := range util.AllMarkets {
		vv := strings.ToUpper(v)
		vv = strings.Replace(vv, "WETH", "ETH", 1)
		ticker, err := getter(vv)
		if err != nil {
			log.Info("get ticker error " + err.Error())
		} else {
			setCache(exchange, v, ticker)
		}
	}
}

func setCache(exchange, market string, ticker Ticker) {
	cacheKey := cachePreKey + exchange + "_" + market
	tickerByte, err := json.Marshal(ticker)
	if err != nil {
		log.Info("marshal ticker json error " + err.Error())
	} else {
		cache.Set(cacheKey , tickerByte, 3600)
	}
}

func NewCollector() *CollectorImpl {
	rst := &CollectorImpl{exs: make([]ExchangeImpl, 0), syncInterval: defaultSyncInterval, cron: cron.New()}

	for k, v := range exchanges {
		exchange := NewExchange(k, v)
		rst.exs = append(rst.exs, exchange)
	}
	return rst
}

func (c *CollectorImpl) Start() {
	// create cron job and exec sync
	c.cron.AddFunc("0 */1 * * * *", updateBinanceCache)
	c.cron.AddFunc("0 */1 * * * *", updateOkexCache)
	c.cron.AddFunc("0 */1 * * * *", updateHuobiCache)
	log.Info("start collect cron jobs......... ")
	c.cron.Start()

}

func (c *CollectorImpl) GetTickers(market string) ([]Ticker, error) {

	result := make([]Ticker, 0)

	for _, e := range c.exs {
		cacheKey := cachePreKey + e.name + "_" + market
		byteRst, err := cache.Get(cacheKey)
		if err != nil {
			var unmarshalRst Ticker
			json.Unmarshal(byteRst, &unmarshalRst)
			result = append(result, unmarshalRst)
		}
	}
	return result, nil
}

//func (c *CollectorImpl) getTickerFromRemote(exchange string) ([] Ticker , error) {
//	return nil, nil
//}
//
//func (c *CollectorImpl) getTickerByMarket(exchange string , market string) (Ticker, error) {
//
//}

type HuobiTicker struct {
	Timestamp int64     `json:"ts"`
	ErrorCode string    `json:"err-code"`
	Status    string    `json:"status"`
	Tick      HuobiInnerTicker `json:"tick"`
}

type HuobiInnerTicker struct {
	Close     float64   `json:"close"`
	Open      float64   `json:"open"`
	High      float64   `json:"high"`
	Low       float64   `json:"low"`
	Amount    float64   `json:"amount"`
	Count     int       `json:"count"`
	Vol       float64   `json:"vol"`
	Ask       []float64 `json:"ask"`
	Bid       []float64 `json:"bid"`
}

type BinanceTicker struct {
	Symbol    string  `json:"symbol"`
	Change    string `json:"priceChangePercent"`
	Close     string `json:"prevClosePrice"`
	Open      string `json:"openPrice"`
	High      string `json:"highPrice"`
	Low       string `json:"lowPrice"`
	LastPrice string `json:"lastPrice"`
	Amount    string `json:"volume"`
	Vol       string `json:"quoteVolume"`
	Ask       string `json:"askPrice"`
	Bid       string `json:"bidPrice"`
}

type OkexTicker struct {
	High      float64 `json:"high"`
	Low       float64 `json:"low"`
	LastPrice float64 `json:"last"`
	Vol       float64 `json:"vol"`
	Ask       float64 `json:"sell"`
	Bid       float64 `json:"buy"`
}

func GetTickerFromHuobi(market string) (ticker Ticker, err error) {

	huobiMarket := strings.Replace(market, "-", "", 1)
	huobiMarket = strings.ToLower(huobiMarket)
	url := fmt.Sprintf(exchanges["huobi"], huobiMarket)
	resp, err := http.Get(url)
	if err != nil {
		return ticker, err
	}
	defer func() {
		if nil != resp && nil != resp.Body {
			resp.Body.Close()
		}
	}()

	body, err := ioutil.ReadAll(resp.Body)
	if nil != err {
		return ticker, err
	} else {
		var huobiTicker *HuobiTicker
		if err := json.Unmarshal(body, &huobiTicker); nil != err {
			return ticker, err
		} else {

			if huobiTicker.Status == "error" {
				return ticker, errors.New("get ticker from huobi error" + huobiTicker.ErrorCode)
			}

			ticker = Ticker{}
			innerTicker := huobiTicker.Tick
			fmt.Println(huobiTicker)
			fmt.Println(innerTicker.Amount)
			fmt.Println(innerTicker.Vol)
			fmt.Println(innerTicker.Bid)
			fmt.Println(innerTicker.Ask)
			ticker.Market = market
			ticker.Amount = innerTicker.Amount
			ticker.Open = innerTicker.Open
			ticker.Close = innerTicker.Close
			ticker.Last = innerTicker.Bid[0]
			ticker.Change = fmt.Sprintf("%.2f%%", 100*(ticker.Last-ticker.Open)/ticker.Open)
			ticker.Exchange = "huobi"
			ticker.Vol = innerTicker.Vol
			ticker.High = innerTicker.High
			ticker.Low = innerTicker.Low

			return ticker, nil
		}
	}
}

func GetTickerFromBinance(market string) (ticker Ticker, err error) {

	binanceMarket := strings.Replace(market, "-", "", 1)
	binanceMarket = strings.ToUpper(binanceMarket)
	url := fmt.Sprintf(exchanges["binance"], binanceMarket)

	resp, err := http.Get(url)
	log.Info("get ticker from " + url)
	if err != nil {
		return ticker, err
	}
	defer func() {
		if nil != resp && nil != resp.Body {
			resp.Body.Close()
		}
	}()

	body, err := ioutil.ReadAll(resp.Body)
	log.Info("get ticker response is " + string(body))
	if nil != err {
		return ticker, err
	} else {
		var binanceTicker *BinanceTicker
		if err := json.Unmarshal(body, &binanceTicker); nil != err {
			return ticker, err
		} else {
			ticker = Ticker{}
			//ticker.Market = market
			ticker.Amount, _ = strconv.ParseFloat(binanceTicker.Amount, 64)
			ticker.Open, _ = strconv.ParseFloat(binanceTicker.Open, 64)
			ticker.Close, _ = strconv.ParseFloat(binanceTicker.Close, 64)
			ticker.Last, _ = strconv.ParseFloat(binanceTicker.LastPrice, 64)
			change, _ := strconv.ParseFloat(binanceTicker.Change, 64)
			ticker.Change = fmt.Sprintf("%.2f%%", change)
			ticker.Exchange = "binance"
			ticker.Vol, _ = strconv.ParseFloat(binanceTicker.Vol, 64)
			ticker.High, _ = strconv.ParseFloat(binanceTicker.High, 64)
			ticker.Low, _ = strconv.ParseFloat(binanceTicker.Low, 64)
			return ticker, nil
		}
	}
}

func GetTickerFromOkex(market string) (ticker Ticker, err error) {

	okexMarket := strings.Replace(market, "-", "_", 1)
	okexMarket = strings.ToLower(okexMarket)
	url := fmt.Sprintf(exchanges["okex"], okexMarket)

	resp, err := http.Get(url)
	log.Info("get ticker from " + url)
	if err != nil {
		return ticker, err
	}
	defer func() {
		if nil != resp && nil != resp.Body {
			resp.Body.Close()
		}
	}()

	body, err := ioutil.ReadAll(resp.Body)
	log.Info("get ticker response is " + string(body))
	if nil != err {
		return ticker, err
	} else {
		var okexTicker *OkexTicker
		if err := json.Unmarshal(body, &okexTicker); nil != err {
			return ticker, err
		} else {
			ticker = Ticker{}
			//ticker.Market = market
			ticker.Last = okexTicker.LastPrice
			ticker.Change = fmt.Sprintf("%.2f%%", 100*(ticker.Last-ticker.Open)/ticker.Open)
			ticker.Exchange = "okex"
			ticker.Vol = okexTicker.Vol
			ticker.High = okexTicker.High
			ticker.Low = okexTicker.Low
			return ticker, nil
		}
	}
}