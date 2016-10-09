package nflwp

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
)

const (
	WPADJUST = iota
	GAMESPLAYED
	GAMESWON
	OPPWPADJUST
	SPREAD
	STDDEV = 13.45
)

type SingleGameInfo struct {
	HomeTeam     string
	VisitingTeam string
	data         []float64
}

type AllTeamData map[string][]float64

func NewAllTeamData() AllTeamData {
	return make(map[string][]float64)
}

func NewTeamData() []float64 {
	TeamData := make([]float64, 5)
	for i := 0; i < len(TeamData)-1; i++ {
		TeamData[i] = 0.0
	}
	TeamData[SPREAD] = -100
	return TeamData
}

func (a AllTeamData) AddData(OtherData AllTeamData) {
	for key, val := range OtherData {
		if _, ok := a[key]; !ok {
			a[key] = NewTeamData()
		}
		for i := 0; i < len(val); i++ {
			a[key][i] += val[i]
		}
	}
}

// Given a probability and actual spread, find an estimated spread
func NewSpread(prob, spread, stdev float64) float64 {
	count := 0
	estimatedSpread := spread
	computedWinProb := WinProbability(0, spread, stdev)
	for math.Abs(computedWinProb-prob) > .001 && count < 1000 {
		if prob > computedWinProb {
			estimatedSpread -= 0.1
		} else {
			estimatedSpread += 0.1
		}
		computedWinProb = WinProbability(0, estimatedSpread, stdev)
		count++
	}
	return estimatedSpread
}

// Given a probability, find an estimated spread
func GuessSpread(prob, stdev float64) float64 {
	count := 0
	estimatedSpread := 0.0
	computedWinProb := 0.5
	for math.Abs(computedWinProb-prob) > .001 && count < 1000 {
		if prob > computedWinProb {
			estimatedSpread -= 0.5
		} else {
			estimatedSpread += 0.5
		}
		computedWinProb = WinProbability(0, estimatedSpread, stdev)
		count++
	}
	return estimatedSpread
}

// Used to calculate cdf(x)
func erfc(x float64) float64 {
	z := math.Abs(x)
	t := 1 / (1 + z/2)
	r := t * math.Exp(-z*z-1.26551223+t*(1.00002368+t*(0.37409196+t*(0.09678418+t*(-0.18628806+t*(0.27886807+t*(-1.13520398+t*(1.48851587+t*(-0.82215223+t*0.17087277)))))))))
	if x >= 0 {
		return r
	} else {
		return 2 - r
	}
}

// Return cdf(x) for the normal distribution based on pro-football-reference win probability.
func cdf(x, mean, stdev float64) float64 {
	return 0.5 * erfc(-(x-mean)/(stdev*math.Sqrt(2)))
}

// Given a spread, calculate the win probability based on pro-football-reference.
func WinProbability(scoreDiff, spread, stdev float64) float64 {
	return 1 - cdf(scoreDiff+0.5, -spread, stdev) + 0.5*(cdf(scoreDiff+0.5, -spread, stdev)-cdf(scoreDiff-0.5, -spread, stdev))
}

// Given a haystack and two needles, return a slice containing all text occuring between
// needle1 and needle2
// Returns nil on error or if nothing is found.
func FindAllBetween(Haystack []byte, Needle1, Needle2 string) []string {
	regex, err := regexp.Compile(Needle1 + "(.*?)" + Needle2)
	if err != nil {
		fmt.Println("Error: ", err)
		return nil
	}
	RepsonseBytes := regex.FindAll(Haystack, -1)
	if RepsonseBytes == nil {
		fmt.Printf("Nothing found with haystack=%v, needle1=%v, and needle2=%v", string(Haystack), Needle1, Needle2)
		return nil
	}
	ResponseStrings := make([]string, len(RepsonseBytes))
	for i := 0; i < len(RepsonseBytes); i++ {
		ResponseStrings[i] = string(RepsonseBytes[i])
	}
	return ResponseStrings
}

func CheckFileExists(filename, url string) []byte {
	var body []byte
	// First we check to see if we have already downloaded this file.
	file, err := os.Open(filename)
	if err != nil {
		if os.IsNotExist(err) {
			response, err := http.Get(url)
			defer response.Body.Close()
			if err != nil {
				fmt.Println("Error: ", err)
				return nil
			}
			body, err = ioutil.ReadAll(response.Body)
			if err != nil {
				fmt.Println("Error: ", err)
				return nil
			}
			file, err = os.Create(filename)
			defer file.Close()
			if err != nil {
				fmt.Println("ERROR: We fetched the data, but there was a problem creating the file.")
				return nil
			}
			_, err = file.Write(body)
			if err != nil {
				fmt.Println("ERROR: Cannot write to file for some reason.")
				return nil
			}
		} else {
			fmt.Println("ERROR: Problem opening file.")
			return nil
		}
	} else {
		body, err = ioutil.ReadAll(file)
		if err != nil {
			fmt.Println("ERROR: Cannot read the file for some reason.")
			return nil
		}
		file.Close()
	}
	return body
}

// Given the spread of a game and the info for a given play,
// calculate the probability the spread predicts at this point of the game
func FindAdjustedStartingProbability(Spread float64, PlayInfo string, PreviousAdjustment float64) float64 {
	var err error
	var Index int
	var Quarter, MinsRemaining, Tmp float64
	TotalMins := 60.0
	if strings.Compare(string(PlayInfo[1]), "O") == 0 {
		TotalMins += 15.0
		Quarter = 4
	} else {
		Quarter, err = strconv.ParseFloat(string(PlayInfo[2]), 64)
		if err != nil {
			if strings.Compare(PlayInfo, "null") != 0 {
				fmt.Println("ERROR1: ", err, PlayInfo)
			}
			return PreviousAdjustment
		}
	}
	PlayInfo = PlayInfo[4:]
	Index = strings.Index(PlayInfo, ":")
	Tmp, err = strconv.ParseFloat(PlayInfo[:Index], 64)
	if err != nil {
		fmt.Println("ERROR2: ", err, PlayInfo)
		return WinProbability(0, Spread, STDDEV/math.Sqrt(TotalMins/((5.0-Quarter)*15.0)))
	}
	MinsRemaining = Tmp
	Tmp, err = strconv.ParseFloat(PlayInfo[Index+1:Index+3], 64)
	if err != nil {
		fmt.Println("ERROR3: ", err, PlayInfo)
		return WinProbability(0, Spread, STDDEV/math.Sqrt(TotalMins/((4.0-Quarter)*15.0+MinsRemaining)))
	}
	MinsRemaining += Tmp / 60.0
	return WinProbability(0, Spread, STDDEV/math.Sqrt(TotalMins/((4.0-Quarter)*15.0+MinsRemaining)))
}

// Given the HTML text of a gamelink, we get the team abbreviations
func GetTeamNames(HTML string) (string, string) {
	WhereToStartLooking := strings.Index(HTML, "vAxis")
	WhereToStopLooking := strings.Index(HTML, "hAxis")
	HTML = HTML[WhereToStartLooking:WhereToStopLooking]
	SplitAtQuote := strings.Split(HTML, "\"")
	return SplitAtQuote[1], SplitAtQuote[len(SplitAtQuote)-2]
}

// Given a link in the format "/boxscore/YYYYMMDD0aaa.htm", we find the data for the given game.
func GetDataForGameLink(Link string) (AllTeamData, string, string) {
	var HomeTeam, VisitingTeam string
	var StartingPercent, ThisPercent, GuessedSpread, ThisPercentAdjustment float64
	var err error
	var TeamData AllTeamData = NewAllTeamData()
	url := "http://www.pro-football-reference.com" + Link
	body := CheckFileExists("NFL"+strings.Replace(Link, "/", "-", -1), url)
	VisitingTeam, HomeTeam = GetTeamNames(string(body))
	Data := FindAllBetween(body, "var chartData = ", "\n")
	if Data == nil {
		fmt.Println("We didn't find the data we need on the provided page so we can't return anything")
		return nil, "", ""
	}
	Data[0] = strings.Replace(Data[0], "var chartData = ", "", -1)
	Data = strings.Split(Data[0][2:len(Data[0])-2], "],[")
	for _, val := range Data {
		ThisPlay := strings.Split(val, ",")
		if strings.Compare(ThisPlay[0], "1") == 0 {
			StartingPercent, err = strconv.ParseFloat(ThisPlay[1], 64)
			if err != nil {
				fmt.Println("Error: ", err)
				return nil, "", ""
			}
			GuessedSpread = GuessSpread(StartingPercent, STDDEV)
			TeamData[HomeTeam] = NewTeamData()
			TeamData[HomeTeam][GAMESPLAYED] = 1.0
			TeamData[VisitingTeam] = NewTeamData()
			TeamData[VisitingTeam][GAMESPLAYED] = 1.0
		}
		ThisPercent, err = strconv.ParseFloat(ThisPlay[1], 64)
		if err != nil {
			fmt.Println("Error: ", err)
			return nil, "", ""
		}
		ThisPercentAdjustment = FindAdjustedStartingProbability(GuessedSpread, ThisPlay[2], ThisPercentAdjustment)
		TeamData[HomeTeam][WPADJUST] += ThisPercent - ThisPercentAdjustment
		TeamData[VisitingTeam][WPADJUST] += ThisPercentAdjustment - ThisPercent
	}
	TeamData[HomeTeam][WPADJUST] = (TeamData[HomeTeam][WPADJUST] / float64(len(Data)))
	TeamData[VisitingTeam][WPADJUST] = (TeamData[VisitingTeam][WPADJUST] / float64(len(Data)))
	if ThisPercent == 1.0 {
		TeamData[HomeTeam][GAMESWON] += 1
	} else {
		TeamData[VisitingTeam][GAMESWON] += 1
	}
	return TeamData, VisitingTeam, HomeTeam
}

// Given a year and week number, returns an AllTeamData with the week's numbers.
// If we incure an error, nil is returned.
func GetTeamDataForWeek(TeamData AllTeamData, Year, Week string) {
	url := "http://www.pro-football-reference.com/years/" + Year + "/week_" + Week + ".htm"
	body := CheckFileExists("NFL-"+Year+"-Week"+Week, url)
	GameURLs := FindAllBetween(body, "gamelink[^h]*href=\"", "\">")
	for _, val := range GameURLs {
		ThisGameLink := FindAllBetween([]byte(val), "/boxscores", ".htm")
		if ThisGameLink == nil {
			fmt.Println("Cannot find a game link in", val)
			continue
		}
		ThisGame, VisitingTeam, HomeTeam := GetDataForGameLink(ThisGameLink[0])
		if ThisGame == nil {
			fmt.Println("Error getting game data for link", ThisGameLink[0])
			continue
		}
		if strings.Compare(Week, "1") != 0 {
			ThisGame[VisitingTeam][OPPWPADJUST] += TeamData[HomeTeam][WPADJUST] / TeamData[HomeTeam][GAMESPLAYED]
			ThisGame[HomeTeam][OPPWPADJUST] += TeamData[VisitingTeam][WPADJUST] / TeamData[VisitingTeam][GAMESPLAYED]
		}
		TeamData.AddData(ThisGame)
	}
}

// Given a year, returns an AllTeamData with the year's numbers
// If StopAtWeek > 0, then we stop gathering data after that week
// If we incure an error, nil is returned
func GetTeamDataForYear(Year string, StopAtWeek int) AllTeamData {
	var TeamData AllTeamData = NewAllTeamData()
	Week := 1
	for StopAtWeek < 0 || Week <= StopAtWeek {
		GetTeamDataForWeek(TeamData, Year, strconv.Itoa(Week))
		Week++
	}
	return TeamData
}

// Translate team names from FootballLocks to pro-football-reference.
func GetPFRTeamAbbr(TeamName string) string {
	return map[string]string{
		"Texans":     "HTX",
		"Patriots":   "NWE",
		"Bengals":    "CIN",
		"Broncos":    "DEN",
		"Titans":     "OTI",
		"Raiders":    "RAI",
		"Cardinals":  "CRD",
		"Bills":      "BUF",
		"Ravens":     "RAV",
		"Jaguars":    "JAX",
		"Dolphins":   "MIA",
		"Browns":     "CLE",
		"Giants":     "NYG",
		"Redskins":   "WAS",
		"Packers":    "GNB",
		"Lions":      "DET",
		"Panthers":   "CAR",
		"Vikings":    "MIN",
		"Seahawks":   "SEA",
		"49ers":      "SFO",
		"Buccaneers": "TAM",
		"Rams":       "RAM",
		"Steelers":   "PIT",
		"Eagles":     "PHI",
		"Chiefs":     "KAN",
		"Jets":       "NYJ",
		"Colts":      "CLT",
		"Chargers":   "SDG",
		"Cowboys":    "DAL",
		"Bears":      "CHI",
		"Saints":     "NOR",
		"Falcons":    "ATL",
	}[TeamName]
}

// Given a completed AllTeamVariable, add the current betting lines from FootballLocks
// and calculate the win probability.
func GetCurrentSpreadsAndWinProb(TeamData AllTeamData) AllTeamData {
	url := "https://fantasydata.com/nfl-stats/nfl-point-spreads-and-odds.aspx"
	response, err := http.Get(url)
	defer response.Body.Close()
	if err != nil {
		fmt.Println("Error: ", err)
		return nil
	}
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		fmt.Println("Error: ", err)
		return nil
	}
	Index := bytes.Index(body, []byte("StatsGrid"))
	body = body[Index:]
	Index = bytes.Index(body, []byte("<tbody>"))
	body = body[Index:]
	Index = bytes.Index(body, []byte("</tbody>"))
	body = body[:Index]
	TableData := FindAllBetween(body, "<td>", "</td>")
	for i := 0; i < len(TableData); i += 6 {
		Favorite := strings.Replace(string(TableData[i]), "at ", "", 1)
		Dog := strings.Replace(string(TableData[i+2]), "at ", "", 1)
		Favorite = strings.Replace(Favorite, "<td>", "", 1)
		Favorite = strings.Replace(Favorite, "</td>", "", 1)
		Favorite = GetPFRTeamAbbr(Favorite)
		Dog = strings.Replace(Dog, "<td>", "", 1)
		Dog = strings.Replace(Dog, "</td>", "", 1)
		Dog = GetPFRTeamAbbr(Dog)
		TableData[i+1] = strings.Replace(TableData[i+1], "<td>", "", 1)
		TableData[i+1] = strings.Replace(TableData[i+1], "</td>", "", 1)
		Spread, err := strconv.ParseFloat(TableData[i+1], 64)
		if err != nil {
			fmt.Printf("It seems that the line for the %v vs %v game is not available because we got %v for the line.\n", Favorite, Dog, TableData[i+1])
		} else {
			TeamData[Favorite][SPREAD] = Spread
			TeamData[Dog][SPREAD] = -Spread
		}
	}
	return TeamData
}
