package nflwp

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

type SingleGameInfo struct {
	HomeTeam     string
	VisitingTeam string
	data         []float64
}

type AllTeamData map[string][]float64

func (a AllTeamData) AddData(OtherData AllTeamData) {
	for key, val := range OtherData {
		if _, ok := a[key]; !ok {
			a[key] = make([]float64, 3)
			a[key][0] = 0.0
			a[key][1] = 0.0
			a[key][2] = 0.0
		}
		a[key][0] += val[0]
		a[key][1] += val[1]
		a[key][2] += val[2]
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

// Given the HTML text of a gamelink, we get the team abbreviations
func GetTeamNames(HTML string) (string, string) {
	WhereToStartLooking := strings.Index(HTML, "vAxis")
	WhereToStopLooking := strings.Index(HTML, "hAxis")
	HTML = HTML[WhereToStartLooking:WhereToStopLooking]
	SplitAtQuote := strings.Split(HTML, "\"")
	return SplitAtQuote[1], SplitAtQuote[len(SplitAtQuote)-2]
}

// Given a link in the format "/boxscore/YYYYMMDD0aaa.htm", we find the data for the given game.
func GetDataForGameLink(Link string) AllTeamData {
	var HomeTeam, VisitingTeam string
	var StartingPercent, ThisPercent float64
	var TeamData AllTeamData = make(map[string][]float64)
	url := "http://www.pro-football-reference.com" + Link
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
	VisitingTeam, HomeTeam = GetTeamNames(string(body))
	Data := FindAllBetween(body, "var chartData = ", "\n")
	if Data == nil {
		fmt.Println("We didn't find the data we need on the provided page so we can't return anything")
		return nil
	}
	Data[0] = strings.Replace(Data[0], "var chartData = ", "", -1)
	Data = strings.Split(Data[0][2:len(Data[0])-2], "],[")
	for _, val := range Data {
		ThisPlay := strings.Split(val, ",")
		if strings.Compare(ThisPlay[0], "1") == 0 {
			StartingPercent, err = strconv.ParseFloat(ThisPlay[1], 64)
			if err != nil {
				fmt.Println("Error: ", err)
				return nil
			}
			TeamData[HomeTeam] = make([]float64, 3)
			TeamData[HomeTeam][0] = 0.0
			TeamData[HomeTeam][1] = 1.0
			TeamData[HomeTeam][2] = 0.0
			TeamData[VisitingTeam] = make([]float64, 3)
			TeamData[VisitingTeam][0] = 0.0
			TeamData[VisitingTeam][1] = 1.0
			TeamData[VisitingTeam][2] = 0.0
		}
		ThisPercent, err = strconv.ParseFloat(ThisPlay[1], 64)
		if err != nil {
			fmt.Println("Error: ", err)
			return nil
		}
		TeamData[HomeTeam][0] += ThisPercent
		TeamData[VisitingTeam][0] += 1.0 - ThisPercent
	}
	TeamData[HomeTeam][0] = (TeamData[HomeTeam][0] / float64(len(Data))) - StartingPercent
	TeamData[VisitingTeam][0] = (TeamData[VisitingTeam][0] / float64(len(Data))) - 1.0 + StartingPercent
	if ThisPercent == 1.0 {
		TeamData[HomeTeam][2] += 1
	} else {
		TeamData[VisitingTeam][2] += 1
	}
	return TeamData
}

// Given a year and week number, returns an AllTeamData with the week's numbers.
// If we incure an error, nil is returned.
func GetTeamDataForWeek(Year, Week string) AllTeamData {
	var TeamData AllTeamData = make(map[string][]float64)
	url := "http://www.pro-football-reference.com/years/" + Year + "/week_" + Week + ".htm"
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
	GameURLs := FindAllBetween(body, "gamelink[^h]*href=\"", "\">")
	for _, val := range GameURLs {
		ThisGameLink := FindAllBetween([]byte(val), "/boxscores", ".htm")
		if ThisGameLink == nil {
			fmt.Println("Cannot find a game link in", val)
			continue
		}
		ThisGame := GetDataForGameLink(ThisGameLink[0])
		if ThisGame == nil {
			fmt.Println("Error getting game data for link", ThisGameLink[0])
			continue
		}
		TeamData.AddData(ThisGame)
	}
	return TeamData
}

// Given a year, returns an AllTeamData with the year's numbers
// If StopAtWeek > 0, then we stop gathering data after that week
// If we incure an error, nil is returned
func GetTeamDataForYear(Year string, StopAtWeek int) AllTeamData {
	var TeamData AllTeamData = make(map[string][]float64)
	Week := 1
	for StopAtWeek < 0 || Week <= StopAtWeek {
		ThisWeek := GetTeamDataForWeek(Year, strconv.Itoa(Week))
		if ThisWeek == nil {
			fmt.Println("Error getting week link for year", Year, "and week", Week)
			if Week > 10 {
				break
			}
			continue
		}
		TeamData.AddData(ThisWeek)
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
			fmt.Println("ERROR: Problem getting the spreads.")
			return TeamData
		}
		TeamData[Favorite] = append(TeamData[Favorite], Spread)
		TeamData[Dog] = append(TeamData[Dog], -Spread)
	}
	return TeamData
}
