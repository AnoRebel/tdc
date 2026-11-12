/*
Command generate rebuilds places.tsv.gz from GeoNames.

Run by hand, never by the build: `go generate ./internal/places` downloads
cities15000 (every town of 15 000 people or more) and the country list, keeps
the name, country and position of each, and writes the compact file the server
embeds. The output is committed, so building the server never touches the
network; rerunning this picks up whatever GeoNames has published since.

The data is GeoNames (https://www.geonames.org), licensed CC BY 4.0. The
attribution lives in NOTICE and docs/hosting.md.

Output format, gzipped UTF-8 text:

	CC<TAB>Country name          one line per country
	<blank line>
	CC<TAB>lat<TAB>lon<TAB>Town  one line per town, lat/lon in hundredths of a degree

Hundredths are about a kilometre, which is finer than the question asked of
them — which town is nearest — and half the size of full precision.
*/
package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	citiesURL    = "https://download.geonames.org/export/dump/cities15000.zip"
	countriesURL = "https://download.geonames.org/export/dump/countryInfo.txt"
)

type town struct {
	country  string
	lat, lon int
	name     string
}

func main() {
	out := flag.String("out", "places.tsv.gz", "where to write the file")
	flag.Parse()

	countries := readCountries(fetch(countriesURL))
	towns := readTowns(unzip(fetch(citiesURL), "cities15000.txt"))

	// Ordered by country then position, which is what compresses best: the
	// country codes repeat and neighbouring numbers share their digits.
	sort.Slice(towns, func(i, j int) bool {
		a, b := towns[i], towns[j]
		if a.country != b.country {
			return a.country < b.country
		}
		if a.lat != b.lat {
			return a.lat < b.lat
		}
		if a.lon != b.lon {
			return a.lon < b.lon
		}
		return a.name < b.name
	})

	codes := make([]string, 0, len(countries))
	for code := range countries {
		codes = append(codes, code)
	}
	sort.Strings(codes)

	var buffer bytes.Buffer
	// A fixed header time, so the same data always produces the same bytes.
	writer, err := gzip.NewWriterLevel(&buffer, gzip.BestCompression)
	if err != nil {
		log.Fatal(err)
	}
	writer.ModTime = time.Unix(0, 0)
	for _, code := range codes {
		fmt.Fprintf(writer, "%s\t%s\n", code, countries[code])
	}
	fmt.Fprintln(writer)
	for _, t := range towns {
		fmt.Fprintf(writer, "%s\t%d\t%d\t%s\n", t.country, t.lat, t.lon, t.name)
	}
	if err := writer.Close(); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out, buffer.Bytes(), 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %d countries and %d towns, %d bytes, to %s",
		len(codes), len(towns), buffer.Len(), *out)
}

func fetch(url string) []byte {
	response, err := http.Get(url) //nolint:gosec,noctx // a fixed URL in a tool run by hand
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		log.Fatalf("%s: %s", url, response.Status)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		log.Fatal(err)
	}
	return body
}

func unzip(archive []byte, name string) []byte {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		log.Fatal(err)
	}
	file, err := reader.Open(name)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	body, err := io.ReadAll(file)
	if err != nil {
		log.Fatal(err)
	}
	return body
}

// readCountries reads countryInfo.txt: ISO code in column 1, name in column 5.
func readCountries(data []byte) map[string]string {
	countries := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) > 4 && fields[0] != "" && fields[4] != "" {
			countries[fields[0]] = fields[4]
		}
	}
	return countries
}

// readTowns reads the GeoNames main table: name in column 2, latitude and
// longitude in 5 and 6, country code in 9.
func readTowns(data []byte) []town {
	var towns []town
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), "\t")
		if len(fields) < 9 || fields[8] == "" {
			continue
		}
		lat, latErr := strconv.ParseFloat(fields[4], 64)
		lon, lonErr := strconv.ParseFloat(fields[5], 64)
		if latErr != nil || lonErr != nil {
			continue
		}
		name := strings.TrimSpace(fields[1])
		if name == "" || strings.ContainsAny(name, "\t\n") {
			continue
		}
		towns = append(towns, town{
			country: fields[8],
			lat:     int(math.Round(lat * 100)),
			lon:     int(math.Round(lon * 100)),
			name:    name,
		})
	}
	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}
	return towns
}
