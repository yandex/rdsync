(defproject jepsen.rdsync "0.1.0-SNAPSHOT"
  :description "rdsync tests"
  :url "https://github.com/yandex/rdsync"
  :java-source-paths ["java"]
  :dependencies [[org.clojure/clojure "1.12.4"]
                 [jepsen "0.3.11"]
                 [redis.clients/jedis "5.2.0" :exclusions [org.slf4j/slf4j-api]]])
