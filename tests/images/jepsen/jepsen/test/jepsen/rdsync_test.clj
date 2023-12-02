(ns jepsen.rdsync-test
  (:require [clojure.test :refer :all]
            [jepsen.core :as jepsen]
            [jepsen.rdsync :as rdsync]))

(def redis_nodes ["redis1" "redis2" "redis3"])

(def zk_nodes ["zoo1" "zoo2" "zoo3"])

(deftest rdsync-test
  (is (:valid? (:results (jepsen/run! (rdsync/rdsync-test redis_nodes zk_nodes))))))
