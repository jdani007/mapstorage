package main

import (
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/storage"
)

type volumeData struct {
	Name   string
	UUID   string
	Size   string
	Server string
	Bucket string
}

func init() {
	if err := loadEnv(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func main() {

	cluster, service, export, err := getFlags()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	creds, err := getCredentials()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	client, err := getStorageClient()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if err := run(cluster, service, creds, export, client); err != nil {
		log.Fatal(err)
	}
}

func run(cluster, service, creds, export string, client *storage.Client) error {
	defer client.Close()

	done := make(chan bool)

	if export == "none" {
		go printDots(service, done)
	}

	var volData []volumeData
	var bucketName string
	var err error

	switch service {
	case "backup":
		bucketName, volData, err = getBackupSize(creds, cluster, client)
		if err != nil {
			return err
		}
	case "tiering":
		bucketName, volData, err = getTieringSize(creds, cluster, client)
		if err != nil {
			return err
		}
	}

	switch export {
	case "local":
		if _, err := createCSV(cluster, service, volData); err != nil {
			return err
		}
	case "cloud":
		fileName, err := createCSV(cluster, service, volData)
		if err != nil {
			return err
		}
		if err := uploadCSV(fileName, bucketName, client); err != nil {
			return err
		}
	default:
		done <- true
		formatOutput(service, volData)
	}

	return nil
}

func getCredentials() (string, error) {

	user, ok := os.LookupEnv("netapp_user")
	if !ok {
		return "", fmt.Errorf("missing environment variable 'netapp_user'")
	}
	pass, ok := os.LookupEnv("netapp_pass")
	if !ok {
		return "", fmt.Errorf("missing environment variable 'netapp_pass'")
	}

	return base64.StdEncoding.EncodeToString([]byte(user + ":" + pass)), nil
}

func getHTTPClient(creds, url string) (*http.Response, error) {

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

	client := &http.Client{
		Timeout:   time.Second * 10,
		Transport: transport,
	}

	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Basic "+creds)
	resp, err := client.Do(request)
	if err == nil {
		if resp.StatusCode == http.StatusOK {
			return resp, nil
		} else {
			return nil, fmt.Errorf(resp.Status)
		}
	}
	return nil, err
}

func getFlags() (string, string, string, error) {

	cluster := flag.String("cluster", "", "Enter cluster hostname or ip.")
	service := flag.String("service", "backup", "Enter 'backup' or 'tiering' to specify the service.")
	export := flag.String("export", "none", "Export a .csv file. Enter 'local' or 'cloud'.")
	flag.Parse()

	if *cluster == "" {
		return "", "", "", fmt.Errorf("enter cluster hostname or ip")
	}
	if *service != "backup" && *service != "tiering" {
		return "", "", "", fmt.Errorf("enter 'backup' or 'tiering' to specify the service")
	}
	if *export != "none" && *export != "local" && *export != "cloud" {
		return "", "", "", fmt.Errorf("enter 'local' or 'cloud' to export a .csv file")
	}

	return *cluster, *service, *export, nil
}

func loadEnv() error {
	file, err := os.Open(".env")
	if err != nil {
		return err
	}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		s := strings.Split(scanner.Text(), "=")
		k := strings.Trim(s[0], "\"")
		v := strings.Trim(s[1], "\"")
		if err := os.Setenv(k, v); err != nil {
			return err
		}
	}
	return nil
}
