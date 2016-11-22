package nflwp

import (
	"bufio"
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
	WPADJUST         = iota  // The average difference between what the vegas win probability is and what acutally happened
	STRAIGHTWPADJUST         // The average difference between what the straight win probability is and what acutally happened
	GAMESPLAYED              // Games the team has played
	GAMESWON                 // Games a team has won
	OPPWPADJUST              // Every game, we add the opponents WPADJUST to the team
	SPREAD                   // Spread for a team
	PLAYINGTHISWEEK          // A float64 that indicates who a team is playing this week
	TOTALDATAPOINTS          // Used to create new TeamData
	STDDEV           = 13.45 //This comes from pro-football.com's Win Probability model
)

type AllTeamData map[string][]float64

func NewAllTeamData() AllTeamData {
	return make(map[string][]float64)
}

func NewTeamData() []float64 {
	TeamData := make([]float64, TOTALDATAPOINTS)
	for i := 0; i < len(TeamData)-1; i++ {
		TeamData[i] = 0.0
	}
	// Set the spread to something ridiculous to easily spot buy weeks
	TeamData[SPREAD] = 0
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

// Given an adjusted win probability and the actual spread of a game,
// find a new adjusted spread
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

// Given an opening probability, find an estimated spread based on pro-football.com's win probability model.
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

// Return cdf(x) for the normal distribution
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

// We want to save time fetching the html.
// Here we check to see if we already have the file.
// If not, we go get it and save it to disk.
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
		Quarter = 4
		TotalMins += 15
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
		return WinProbability(0, Spread*((4.0-Quarter)*15.0+MinsRemaining)/TotalMins, STDDEV/math.Sqrt(TotalMins/((4.0-Quarter)*15.0+MinsRemaining)))
	}
	MinsRemaining += Tmp / 60
	AdjustmentFactor := TotalMins / ((4.0-Quarter)*15.0 + MinsRemaining)
	return WinProbability(Spread*(1-(1/AdjustmentFactor)), Spread/AdjustmentFactor, STDDEV/math.Sqrt(AdjustmentFactor))
}

// Given the HTML text of a gamelink, we get the team abbreviations
func GetTeamNames(HTML string) (string, string) {
	WhereToStartLooking := strings.Index(HTML, "vAxis")
	WhereToStopLooking := strings.Index(HTML, "hAxis")
	HTML = HTML[WhereToStartLooking:WhereToStopLooking]
	SplitAtQuote := strings.Split(HTML, "\"")
	return SplitAtQuote[1], SplitAtQuote[len(SplitAtQuote)-2]
}

func GetSpreadFromProFootballPage(body []byte, VisitingTeam, HomeTeam string) float64 {
	IndexOfLine := bytes.Index(body, []byte("Vegas Line"))
	Spread := 1234.0
	var err error
	if IndexOfLine != -1 {
		body = body[IndexOfLine : IndexOfLine+bytes.Index(body[IndexOfLine:], []byte("</td>"))]
		SpreadAsString := string(bytes.Split(body, []byte(" "))[len(bytes.Split(body, []byte(" ")))-1])
		SpreadAsString = strings.TrimSpace(SpreadAsString)
		Spread, err = strconv.ParseFloat(SpreadAsString, 64)
		if err != nil {
			SpreadAsString = strings.Replace(SpreadAsString, ">", "", 1)
			if strings.Compare(SpreadAsString, "Pick") == 0 {
				return 0
			}
			fmt.Printf("ERROR: Error getting the spreads for game %v at %v: %v.\n", VisitingTeam, HomeTeam, SpreadAsString)
			return 1234
		}
	}
	return Spread
}

// pro-football-reference.com puts the spreads for the game on the page after the game starts.
// Here, we peek at the next week to get the spreads.
func PeekAheadForSpreads(TeamData AllTeamData, Year, Week string) AllTeamData {
	var HomeTeam, VisitingTeam string
	url := "http://www.pro-football-reference.com/years/" + Year + "/week_" + Week + ".htm"
	body := CheckFileExists("NFL-"+Year+"-Week"+Week, url)
	GameURLs := FindAllBetween(body, "gamelink[^h]*href=\"", "\">")
	for _, val := range GameURLs {
		ThisGameLink := FindAllBetween([]byte(val), "/boxscores", ".htm")
		if ThisGameLink == nil {
			fmt.Println("Cannot find a game link in ", val)
			continue
		}
		url := "http://www.pro-football-reference.com" + ThisGameLink[0]
		body := CheckFileExists("NFL"+strings.Replace(ThisGameLink[0], "/", "-", -1), url)
		VisitingTeam, HomeTeam = GetTeamNames(string(body))
		Spread := GetSpreadFromProFootballPage(body, VisitingTeam, HomeTeam)
		TeamData[HomeTeam][SPREAD] = Spread
		TeamData[VisitingTeam][SPREAD] = -Spread
		TeamData[HomeTeam][PLAYINGTHISWEEK] = GetTeamFloatFromAbbr(VisitingTeam)
		TeamData[VisitingTeam][PLAYINGTHISWEEK] = GetTeamFloatFromAbbr(HomeTeam)
	}
	return TeamData
}

// Given a link in the format "/boxscore/YYYYMMDD0aaa.htm", we find the data for the given game.
// To save time, we download the html file for later reference.
func GetDataForGameLink(Link string) (AllTeamData, string, string) {
	var HomeTeam, VisitingTeam string
	var StartingPercent, ThisPercent, GuessedSpread, ThisPercentAdjustment float64
	var err error
	var TeamData AllTeamData = NewAllTeamData()
	HaveTeamNames := false
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
		if !HaveTeamNames {
			StartingPercent, err = strconv.ParseFloat(ThisPlay[1], 64)
			if err != nil {
				fmt.Println("Error: ", err)
				return nil, "", ""
			}
			GuessedSpread = GetSpreadFromProFootballPage(body, VisitingTeam, HomeTeam)
			TeamData[HomeTeam] = NewTeamData()
			TeamData[HomeTeam][GAMESPLAYED] = 1.0
			TeamData[VisitingTeam] = NewTeamData()
			TeamData[VisitingTeam][GAMESPLAYED] = 1.0
			HaveTeamNames = true
		}
		ThisPercent, err = strconv.ParseFloat(ThisPlay[1], 64)
		if err != nil {
			fmt.Println("Error: ", err)
			return nil, "", ""
		}
		ThisPercentAdjustment = FindAdjustedStartingProbability(GuessedSpread, ThisPlay[2], ThisPercentAdjustment)
		TeamData[HomeTeam][WPADJUST] += ThisPercent - ThisPercentAdjustment
		TeamData[VisitingTeam][WPADJUST] += ThisPercentAdjustment - ThisPercent
		TeamData[HomeTeam][STRAIGHTWPADJUST] += ThisPercent - StartingPercent + 0.5
		TeamData[VisitingTeam][STRAIGHTWPADJUST] += StartingPercent - ThisPercent + 0.5
	}
	TeamData[HomeTeam][WPADJUST] /= float64(len(Data))
	TeamData[VisitingTeam][WPADJUST] /= float64(len(Data))
	TeamData[HomeTeam][STRAIGHTWPADJUST] /= float64(len(Data))
	TeamData[VisitingTeam][STRAIGHTWPADJUST] /= float64(len(Data))
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
		_, ok := TeamData[VisitingTeam]
		_, ok2 := TeamData[HomeTeam]
		if ok && ok2 {
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
	TeamData["BYE"] = NewTeamData()
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
		"TEXANS":     "HTX",
		"PATRIOTS":   "NWE",
		"BENGALS":    "CIN",
		"BRONCOS":    "DEN",
		"TITANS":     "OTI",
		"RAIDERS":    "RAI",
		"CARDINALS":  "CRD",
		"BILLS":      "BUF",
		"RAVENS":     "RAV",
		"JAGUARS":    "JAX",
		"DOLPHINS":   "MIA",
		"BROWNS":     "CLE",
		"GIANTS":     "NYG",
		"REDSKINS":   "WAS",
		"PACKERS":    "GNB",
		"LIONS":      "DET",
		"PANTHERS":   "CAR",
		"VIKINGS":    "MIN",
		"SEAHAWKS":   "SEA",
		"49ERS":      "SFO",
		"BUCCANEERS": "TAM",
		"RAMS":       "RAM",
		"STEELERS":   "PIT",
		"EAGLES":     "PHI",
		"CHIEFS":     "KAN",
		"JETS":       "NYJ",
		"COLTS":      "CLT",
		"CHARGERS":   "SDG",
		"COWBOYS":    "DAL",
		"BEARS":      "CHI",
		"SAINTS":     "NOR",
		"FALCONS":    "ATL",
	}[TeamName]
}

// This function returns a float for storage in the TeamData type
func GetTeamAbbrFromFloat(Index float64) string {
	return map[float64]string{
		0:  "BYE",
		1:  "HTX",
		2:  "NWE",
		3:  "CIN",
		4:  "DEN",
		5:  "OTI",
		6:  "RAI",
		7:  "CRD",
		8:  "BUF",
		9:  "RAV",
		10: "JAX",
		11: "MIA",
		12: "CLE",
		13: "NYG",
		14: "WAS",
		15: "GNB",
		16: "DET",
		17: "CAR",
		18: "MIN",
		19: "SEA",
		20: "SFO",
		21: "TAM",
		22: "RAM",
		23: "PIT",
		24: "PHI",
		25: "KAN",
		26: "NYJ",
		27: "CLT",
		28: "SDG",
		29: "DAL",
		30: "CHI",
		31: "NOR",
		32: "ATL",
	}[Index]
}

// This function returns the team abbreviation from a float64 stored in the TeamData type
func GetTeamFloatFromAbbr(Abbr string) float64 {
	return map[string]float64{
		"BYE": 0,
		"HTX": 1,
		"NWE": 2,
		"CIN": 3,
		"DEN": 4,
		"OTI": 5,
		"RAI": 6,
		"CRD": 7,
		"BUF": 8,
		"RAV": 9,
		"JAX": 10,
		"MIA": 11,
		"CLE": 12,
		"NYG": 13,
		"WAS": 14,
		"GNB": 15,
		"DET": 16,
		"CAR": 17,
		"MIN": 18,
		"SEA": 19,
		"SFO": 20,
		"TAM": 21,
		"RAM": 22,
		"PIT": 23,
		"PHI": 24,
		"KAN": 25,
		"NYJ": 26,
		"CLT": 27,
		"SDG": 28,
		"DAL": 29,
		"CHI": 30,
		"NOR": 31,
		"ATL": 32,
	}[Abbr]
}

// This takes the spread information I scraped from scoresandodds.com and
// creates data to use with a machine learning algorithm
func CreateDataFromSpreadFiles(Sport string) {
	YearToStart := 2015
	YearToStop := 2015
	FileToWrite, _ := os.Create(Sport + "WPData.txt")
	defer FileToWrite.Close()
	for YearToStart <= YearToStop {
		fmt.Printf("Now compiling stats for %v year...\n", YearToStart)
		var TeamData AllTeamData = NewAllTeamData()
		if strings.Compare(Sport, "Football") == 0 {
			TeamData["BYE"] = NewTeamData()
		}
		file, err := os.Open(strconv.Itoa(YearToStart) + Sport + "OddsAndScores.txt")
		if err != nil {
			fmt.Printf("ERROR: error reading file for year %v and sport %v\n", YearToStart, Sport)
			file.Close()
			return
		}
		scan := bufio.NewScanner(file)
		for scan.Scan() {
			Games := strings.Split(scan.Text(), ",")
			DateString := Games[0]
			Games = Games[1 : len(Games)-1]
			for _, val := range Games {
				GameData := strings.Split(val, " ")
				if len(GameData) < 7 {
					continue
				}
				HomeTeam := GetPFRTeamAbbr(GameData[3])
				VisitingScore, _ := strconv.ParseFloat(GameData[2], 64)
				HomeScore, _ := strconv.ParseFloat(GameData[5], 64)
				Spread, _ := strconv.ParseFloat(GameData[1], 64)
				if Spread < 0 {
					Spread = -Spread
				}
				if Spread < -60 || Spread > 60 {
					Spread, _ = strconv.ParseFloat(GameData[4], 64)
					if Spread > 0 {
						Spread = -Spread
					}
				}
				//StartingWP := WinProbability(0, Spread, STDDEV)
				ThisGame, VisitingTeam, _ := GetDataForGameLink("/boxscores/" + DateString + "0" + strings.ToLower(HomeTeam) + ".htm")
				if ThisGame == nil {
					fmt.Println("Error getting game data for link", DateString+strings.ToLower(HomeTeam)+".htm")
					continue
				}
				if _, ok := TeamData[HomeTeam]; ok {
					if TeamData[HomeTeam][GAMESPLAYED] > 2 {
						GuessSpread := TeamData[HomeTeam][STRAIGHTWPADJUST]/TeamData[HomeTeam][GAMESPLAYED] - TeamData[VisitingTeam][STRAIGHTWPADJUST]/TeamData[VisitingTeam][GAMESPLAYED]
						GuessOP := (-TeamData[HomeTeam][OPPWPADJUST]/(TeamData[HomeTeam][GAMESPLAYED]-1) + TeamData[VisitingTeam][OPPWPADJUST]/(TeamData[VisitingTeam][GAMESPLAYED]-1)) / 2
						GuessWP := (-TeamData[VisitingTeam][WPADJUST]/TeamData[VisitingTeam][GAMESPLAYED] + TeamData[HomeTeam][WPADJUST]/TeamData[HomeTeam][GAMESPLAYED]) / 2
						GuessBoth := (GuessWP + GuessOP) / 2.0
						GuessWP = NewSpread(0.5+GuessWP+GuessSpread, 0.0, STDDEV)
						GuessOP = NewSpread(0.5+GuessOP+GuessSpread, 0.0, STDDEV)
						GuessBoth = NewSpread(0.5+GuessBoth+GuessSpread, 0.0, STDDEV)
						GuessSpread = NewSpread(0.5+GuessSpread, 0.0, STDDEV)
						NewProb := WinProbability(0, TeamData[HomeTeam][SPREAD], STDDEV) + ((TeamData[HomeTeam][WPADJUST]/TeamData[HomeTeam][GAMESPLAYED])-(TeamData[VisitingTeam][WPADJUST]/TeamData[VisitingTeam][GAMESPLAYED]))/2
						EstSpread := NewSpread(NewProb, TeamData[HomeTeam][SPREAD], STDDEV)
						FileToWrite.Write([]byte(strconv.FormatFloat(GuessSpread, 'f', -1, 64)))
						FileToWrite.Write([]byte(","))
						FileToWrite.Write([]byte(strconv.FormatFloat(GuessWP, 'f', -1, 64)))
						FileToWrite.Write([]byte(","))
						FileToWrite.Write([]byte(strconv.FormatFloat(GuessOP, 'f', -1, 64)))
						FileToWrite.Write([]byte(","))
						FileToWrite.Write([]byte(strconv.FormatFloat(GuessBoth, 'f', -1, 64)))
						FileToWrite.Write([]byte(","))
						FileToWrite.Write([]byte(strconv.FormatFloat(EstSpread, 'f', -1, 64)))
						FileToWrite.Write([]byte(","))
						FileToWrite.Write([]byte(strconv.FormatFloat((GuessSpread+GuessWP+GuessOP+GuessBoth+EstSpread)/5, 'f', -1, 64)))
						FileToWrite.Write([]byte(","))
						FileToWrite.Write([]byte(strconv.FormatFloat(Spread, 'f', -1, 64)))
						FileToWrite.Write([]byte(","))
						if HomeScore-VisitingScore+Spread > 0 {
							FileToWrite.Write([]byte("1"))
						} else if HomeScore-VisitingScore+Spread < 0 {
							FileToWrite.Write([]byte("0"))
						} else {
							FileToWrite.Write([]byte("2"))
						}
						FileToWrite.Write([]byte("\n"))
					}
					ThisGame[VisitingTeam][OPPWPADJUST] += TeamData[HomeTeam][WPADJUST] / TeamData[HomeTeam][GAMESPLAYED]
					ThisGame[HomeTeam][OPPWPADJUST] += TeamData[VisitingTeam][WPADJUST] / TeamData[VisitingTeam][GAMESPLAYED]
				}
				TeamData.AddData(ThisGame)
			}
		}
		file.Close()
		YearToStart++
	}
}

// Given a completed AllTeamVariable, we add the current betting lines from FootballLocks
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
		Favorite = GetPFRTeamAbbr(strings.ToUpper(Favorite))
		Dog = strings.Replace(Dog, "<td>", "", 1)
		Dog = strings.Replace(Dog, "</td>", "", 1)
		Dog = GetPFRTeamAbbr(strings.ToUpper(Dog))
		TableData[i+1] = strings.Replace(TableData[i+1], "<td>", "", 1)
		TableData[i+1] = strings.Replace(TableData[i+1], "</td>", "", 1)
		Spread, err := strconv.ParseFloat(TableData[i+1], 64)
		if err != nil {
			fmt.Printf("It seems that the line for the %v vs %v game is not available because we got %v for the line.\n", Favorite, Dog, TableData[i+1])
		} else {
			TeamData[Favorite][SPREAD] = Spread
			TeamData[Dog][SPREAD] = -Spread
			TeamData[Favorite][PLAYINGTHISWEEK] = GetTeamFloatFromAbbr(Dog)
			TeamData[Dog][PLAYINGTHISWEEK] = GetTeamFloatFromAbbr(Favorite)
		}
	}
	return TeamData
}
