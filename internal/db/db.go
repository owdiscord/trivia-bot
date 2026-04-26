// Package db contains our so-called "database", which is primarily a CSV file with the input data
// and a JSON file with the current scores, secured with a mutex to prevent the most obvious of corruptions.
package db
