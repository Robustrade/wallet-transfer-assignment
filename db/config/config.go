package config

import (
	"fmt"

	"github.com/spf13/viper"
)

func Configuration() string {
	viper.SetConfigFile("db/config/.env")
	err := viper.ReadInConfig()
	fmt.Println("err ", err)
	user := viper.Get("user").(string)
	host := viper.Get("host").(string)
	password := viper.Get("password").(string)
	dbname := viper.Get("dbname").(string)
	port := viper.Get("port").(string)
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Shanghai",
		host, user, password, dbname, port)
	return dsn
}
