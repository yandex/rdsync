(ns jepsen.rdsync-test
  (:require [clojure.test :refer :all]
            [jepsen.core :as jepsen]
            [jepsen.rdsync :as rdsync]))

(def valkey_nodes ["valkey1" "valkey2" "valkey3"])

(def zk_nodes ["zoo1" "zoo2" "zoo3"])

(deftest rdsync-test
  (is (:valid? (:results (jepsen/run! (rdsync/rdsync-test valkey_nodes zk_nodes))))))
