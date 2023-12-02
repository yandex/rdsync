(ns jepsen.rdsync
  "Tests for rdsync"
  (:require [clojure.tools.logging :refer :all]
            [clojure.core.reducers :as r]
            [clojure.set :as set]
            [clojure.string :as string]
            [jepsen [tests :as tests]
                    [os :as os]
                    [db :as db]
                    [client :as client]
                    [control :as control]
                    [nemesis :as nemesis]
                    [generator :as gen]
                    [checker :as checker]
                    [util :as util :refer [timeout]]
                    [net :as net]]
            [knossos [op :as op]]
            [zookeeper :as zk])
  (:import [jepsen.rdsync.waitquorum WaitQuorumJedis]))

(def register (atom 0))

(defn noop-client
  "Noop client"
  []
  (reify client/Client
    (setup! [_ test]
      (info "noop-client setup"))
    (invoke! [this test op]
      (assoc op :type :info, :error "noop"))
    (close! [_ test])
    (teardown! [_ test] (info "teardown"))
    client/Reusable
    (reusable? [_ test] true)))

(defn redis-client
  "Redis client"
  [node]
  (reify client/Client
    (setup! [_ test]
      (info "redis-client setup"))
    (open! [_ test node]
      (cond (not (string/includes? (name node) "zoo"))
            (redis-client node)
            true
            (noop-client)))

    (invoke! [this test op]
      (try
        (timeout 5000 (assoc op :type :info, :error "timeout")
                 (let [conn (new WaitQuorumJedis (str "redis://:functestpassword@" (name node) ":6379/"))]
                  (case (:f op)
                    :read (assoc op :type :ok,
                                    :value (->> (.keys conn "*") (vec) (map #(Long/parseLong %)) (set)))
                    :add (do
                       (info (str "Adding: " (get op :value) " to " (name node)))
                       (. conn set (.getBytes (str (get op :value))) (.getBytes "1"))
                       (info (str "Wrote " (get op :value) " to quorum of " (. conn waitQuorum) " replicas"))
                       (assoc op :type :ok)))))
        (catch Throwable t#
          (let [m# (.getMessage t#)]
            (do
              (warn (str "Command error: " m# " on adding: " (get op :value)))
              (assoc op :type :info, :error m#)
                    )))))

    (close! [_ test])
    (teardown! [_ test])
    client/Reusable
    (reusable? [_ test] true)))

(defn db
  "Redis database"
  []
  (reify db/DB
    (setup! [_ test node]
      (info (str (name node) " setup")))

    (teardown! [_ test node]
      (info (str (name node) " teardown")))))

(defn r [_ _] {:type :invoke, :f :read, :value nil})
(defn a [_ _] {:type :invoke, :f :add, :value (swap! register (fn [current-state] (+ current-state 1)))})

(def rdsync-set
  "Given a set of :add operations followed by a final :read, verifies that
  every successfully added element is present in the read, and that the read
  contains only elements for which an add was attempted."
  (reify checker/Checker
    (check [this test history opts]
      (let [attempts (->> history
                          (r/filter op/invoke?)
                          (r/filter #(= :add (:f %)))
                          (r/map :value)
                          (into #{}))
            adds (->> history
                      (r/filter op/ok?)
                      (r/filter #(= :add (:f %)))
                      (r/map :value)
                      (into #{}))
            final-read (->> history
                          (r/filter op/ok?)
                          (r/filter #(= :read (:f %)))
                          (r/map :value)
                          (reduce (fn [_ x] x) nil))]
        (if-not final-read
          {:valid? false
           :error  "Set was never read"}

          (let [; The OK set is every read value which we tried to add
                ok          (set/intersection final-read attempts)

                ; Unexpected records are those we *never* attempted.
                unexpected  (set/difference final-read attempts)

                ; Lost records are those we definitely added but weren't read
                lost        (set/difference adds final-read)

                ; Recovered records are those where we didn't know if the add
                ; succeeded or not, but we found them in the final set.
                recovered   (set/difference ok adds)]

            {:valid?          (and (empty? lost) (empty? unexpected))
             :ok              (util/integer-interval-set-str ok)
             :lost            (util/integer-interval-set-str lost)
             :unexpected      (util/integer-interval-set-str unexpected)
             :recovered       (util/integer-interval-set-str recovered)
             :ok-frac         (util/fraction (count ok) (count attempts))
             :unexpected-frac (util/fraction (count unexpected) (count attempts))
             :lost-frac       (util/fraction (count lost) (count attempts))
             :recovered-frac  (util/fraction (count recovered) (count attempts))}))))))

(defn switcher
  "Executes switchover"
  []
  (reify nemesis/Nemesis
    (setup! [this test]
      this)
    (invoke! [this test op]
             (case (:f op)
               :switch (assoc op :value
                          (try
                              (let [node (rand-nth (filter (fn [x] (string/includes? (name x) "redis"))
                                                           (:nodes test)))]
                                (control/on node
                                  (control/exec :timeout :10 :rdsync :switch :--to node))
                                (assoc op :value [:switchover :on node]))
                            (catch Throwable t#
                              (let [m# (.getMessage t#)]
                                (do (warn (str "Unable to run switch: "
                                               m#))
                                    m#)))))))
    (teardown! [this test]
      (info (str "Stopping switcher")))
    nemesis/Reflection
    (fs [this] #{})))

(defn killer
  "Kills rdsync"
  []
  (reify nemesis/Nemesis
    (setup! [this test]
      this)
    (invoke! [this test op]
             (case (:f op)
               :kill (assoc op :value
                      (try
                          (let [node (rand-nth (filter (fn [x] (string/includes? (name x) "redis"))
                                                       (:nodes test)))]
                            (control/on node
                              (control/exec :supervisorctl :signal :KILL :rdsync))
                            (assoc op :value [:kill :rdsync :on node]))
                        (catch Throwable t#
                          (let [m# (.getMessage t#)]
                            (do (warn (str "Unable to kill rdsync: "
                                           m#))
                                m#)))))))
    (teardown! [this test]
      (info (str "Stopping killer")))
    nemesis/Reflection
    (fs [this] #{})))

(def nemesis-starts [:start-halves :start-ring :start-one :switch :kill])

(defn rdsync-test
  [redis-nodes zookeeper-nodes]
  {:nodes     (concat redis-nodes zookeeper-nodes)
   :name      "rdsync"
   :os        os/noop
   :db        (db)
   :ssh       {:private-key-path "/root/.ssh/id_rsa" :strict-host-key-checking :no :password ""}
   :net       net/iptables
   :client    (redis-client nil)
   :nemesis   (nemesis/compose {{:start-halves :start} (nemesis/partition-random-halves)
                                {:start-ring   :start} (nemesis/partition-majorities-ring)
                                {:start-one    :start
                                 ; All partitioners heal all nodes on stop so we define stop once
                                 :stop         :stop} (nemesis/partition-random-node)
                                #{:switch} (switcher)
                                #{:kill} (killer)})
   :generator (gen/phases
                (->> a
                     (gen/stagger 1/50)
                     (gen/nemesis
                       (fn [] (map gen/once
                                   [{:type :info, :f (rand-nth nemesis-starts)}
                                    {:type :info, :f (rand-nth nemesis-starts)}
                                    {:type :sleep, :value 60}
                                    {:type :info, :f :stop}
                                    {:type :sleep, :value 60}])))
                     (gen/time-limit 3600))
                (->> r
                     (gen/stagger 1)
                     (gen/nemesis
                       (fn [] (map gen/once
                                   [{:type :info, :f :stop}
                                    {:type :sleep, :value 60}])))
                     (gen/time-limit 600)))
   :checker   rdsync-set
   :remote    control/ssh})
