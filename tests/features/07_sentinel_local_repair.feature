Feature: Sentinel mode local node repair

    Scenario: Sentinel mode node overflowed with connections gets reserved connections watchdog triggered
        Given sentinel shard is up and running
        Then valkey host "valkey1" should be master
        And valkey host "valkey2" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey2" should run fine within "15" seconds
        And valkey host "valkey3" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey3" should run fine within "15" seconds
        And zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["valkey1","valkey2","valkey3"]
        """
        When I run command on host "valkey1"
        """
            supervisorctl pid valkey
        """
        And I save command output as "pid_before_conn_exhaustion"
        And I run command on host "valkey1"
        """
            supervisorctl signal STOP rdsync
        """
        And I run command on host "valkey2"
        """
            supervisorctl signal STOP rdsync
        """
        And I run command on host "valkey3"
        """
            supervisorctl signal STOP rdsync
        """
        And I run async command on host "valkey1"
        """
            bash -c 'for _ in {0..1100}; do nc localhost 6379 & done; sleep infinity'
        """
        Then valkey host "valkey1" should become unavailable within "30" seconds
        When I run command on host "valkey1"
        """
            supervisorctl signal CONT rdsync
        """
        And I run command on host "valkey2"
        """
            supervisorctl signal CONT rdsync
        """
        And I run command on host "valkey3"
        """
            supervisorctl signal CONT rdsync
        """
        Then valkey host "valkey1" should become available within "60" seconds
        When I run command on host "valkey1"
        """
            supervisorctl pid valkey
        """
        Then command output should match regexp
        """
            {{.pid_before_conn_exhaustion}}
        """
        When I run command on host "valkey1"
        """
            grep Killing /var/log/rdsync.log
        """
        Then command output should match regexp
        """
            .*Local node has .* free connections left. Killing all client connections.*
        """

    Scenario: Sentinel mode senticache is restarted after OOM
        Given sentinel shard is up and running
        Then valkey host "valkey1" should be master
        And valkey host "valkey2" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey2" should run fine within "15" seconds
        And valkey host "valkey3" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey3" should run fine within "15" seconds
        And zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["valkey1","valkey2","valkey3"]
        """
        When I run command on host "valkey1"
        """
            supervisorctl stop senticache
        """
        Then senticache host "valkey1" should have master "valkey1" within "30" seconds

    Scenario: Busy sentinel mode node gets a SCRIPT KILL
        Given sentinel shard is up and running
        Then valkey host "valkey1" should be master
        And valkey host "valkey2" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey2" should run fine within "15" seconds
        And valkey host "valkey3" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey3" should run fine within "15" seconds
        And zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["valkey1","valkey2","valkey3"]
        """
        When I run async command on host "valkey1"
        """
            valkey-cli -a functestpassword eval 'while true do end' 0
        """
        Then valkey host "valkey1" should become available within "60" seconds

    Scenario: Sentinel mode replica is restarted after OOM
        Given sentinel shard is up and running
        Then valkey host "valkey1" should be master
        And valkey host "valkey2" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey2" should run fine within "15" seconds
        And valkey host "valkey3" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey3" should run fine within "15" seconds
        And zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["valkey1","valkey2","valkey3"]
        """
        When valkey on host "valkey2" is killed
        And I wait for "300" seconds
        Then valkey host "valkey2" should become available within "120" seconds
        And valkey host "valkey2" should become replica of "valkey1" within "60" seconds
        And replication on valkey host "valkey2" should run fine within "60" seconds

    Scenario: Sentinel mode loading replica is not restarted
        Given sentinel shard is up and running
        Then valkey host "valkey1" should be master
        And valkey host "valkey2" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey2" should run fine within "15" seconds
        And valkey host "valkey3" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey3" should run fine within "15" seconds
        And zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["valkey1","valkey2","valkey3"]
        """
        When I run command on host "valkey1" with timeout "180" seconds
        """
            valkey-cli -a functestpassword DEBUG populate 10000000 key 100
        """
        And I run command on valkey host "valkey2"
        """
            CONFIG SET key-load-delay 50
        """
        And I run command on valkey host "valkey2"
        """
            CONFIG SET loading-process-events-interval-bytes 1024
        """
        And I run command on valkey host "valkey2"
        """
            CONFIG REWRITE
        """
        And I run async command on host "valkey2"
        """
            supervisorctl restart valkey
        """
        Then valkey host "valkey2" should become unavailable within "30" seconds
        When I run command on host "valkey2"
        """
            supervisorctl pid valkey
        """
        And I save command output as "pid_right_after_restart"
        And I wait for "360" seconds
        And I run command on host "valkey2"
        """
            supervisorctl pid valkey
        """
        Then command output should match regexp
        """
            {{.pid_right_after_restart}}
        """

    Scenario: Sentinel mode master is restarted after hanging
        Given sentinel shard is up and running
        Then valkey host "valkey1" should be master
        And valkey host "valkey2" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey2" should run fine within "15" seconds
        And valkey host "valkey3" should become replica of "valkey1" within "15" seconds
        And replication on valkey host "valkey3" should run fine within "15" seconds
        And zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["valkey1","valkey2","valkey3"]
        """
        When I run async command on host "valkey1"
        """
            valkey-cli -a functestpassword DEBUG SLEEP 600
        """
        And I wait for "420" seconds
        Then valkey host "valkey1" should become available within "60" seconds
