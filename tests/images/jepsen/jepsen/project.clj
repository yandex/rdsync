(defproject jepsen.rdsync "0.1.0-SNAPSHOT"
  :description "rdsync tests"
  :url "https://github.com/yandex/rdsync"
  :java-source-paths ["java"]
  :dependencies [[org.clojure/clojure "1.10.3"]
                 [org.clojure/tools.nrepl "0.2.13"]
                 [clojure-complete "0.2.5"]
                 [jepsen "0.2.6"]
                 [zookeeper-clj "0.9.4"]
                 [redis.clients/jedis "5.0.0"]])
