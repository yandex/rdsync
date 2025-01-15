Feature: Cluster mode failover from dead master

    Scenario: Cluster mode failover from dead master works
        Given clustered shard is up and running
        Then zookeeper node "/test/health/valkey1" should match json within "30" seconds
        """
        {
            "ping_ok": true,
            "is_master": true,
            "is_read_only": false
        }
        """
        And zookeeper node "/test/health/valkey2" should match json within "30" seconds
        """
        {
            "ping_ok": true,
            "is_master": false
        }
        """
        And zookeeper node "/test/health/valkey3" should match json within "30" seconds
        """
        {
            "ping_ok": true,
            "is_master": false
        }
        """
        When host "valkey1" is stopped
        Then valkey host "valkey1" should become unavailable within "10" seconds
        And  zookeeper node "/test/manager" should match regexp within "30" seconds
        """
            .*valkey[23].*
        """
        And zookeeper node "/test/last_switch" should match json within "60" seconds
        """
        {
            "cause": "auto",
            "from": "valkey1",
            "result": {
                "ok": true
            }
        }
        """
        When I get zookeeper node "/test/master"
        And I save zookeeper query result as "new_master"
        Then valkey host "{{.new_master}}" should be master
        When host "valkey1" is started
        Then valkey host "valkey1" should become available within "20" seconds
        And valkey host "valkey1" should become replica of "{{.new_master}}" within "30" seconds

    Scenario: Cluster mode failover does not work in absence of quorum
        Given clustered shard is up and running
        Then zookeeper node "/test/health/valkey1" should match json within "30" seconds
        """
        {
            "ping_ok": true,
            "is_master": true,
            "is_read_only": false
        }
        """
        And zookeeper node "/test/health/valkey2" should match json within "30" seconds
        """
        {
            "ping_ok": true,
            "is_master": false
        }
        """
        And zookeeper node "/test/health/valkey3" should match json within "30" seconds
        """
        {
            "ping_ok": true,
            "is_master": false
        }
        """
        When valkey on host "valkey1" is killed
        And valkey on host "valkey2" is killed
        Then valkey host "valkey1" should become unavailable within "10" seconds
        And valkey host "valkey2" should become unavailable within "10" seconds
        When I wait for "60" seconds
        Then valkey host "valkey3" should be replica of "valkey1"
        And zookeeper node "/test/master" should match regexp
        """
            valkey1
        """
        And zookeeper node "/test/manager" should match regexp
        """
            valkey1
        """
        When I run command on host "valkey1"
        """
            grep Failover /var/log/rdsync.log
        """
        Then command output should match regexp
        """
            .*Failover was not approved.* .*no quorum.*
        """

    Scenario: Cluster mode failover selects active replica based on priority
        Given clustered shard is up and running
        Then zookeeper node "/test/health/valkey1" should match json within "30" seconds
        """
        {
            "ping_ok": true,
            "is_master": true,
            "is_read_only": false
        }
        """
        And zookeeper node "/test/health/valkey2" should match json within "30" seconds
        """
        {
            "ping_ok": true,
            "is_master": false
        }
        """
        And zookeeper node "/test/health/valkey3" should match json within "30" seconds
        """
        {
            "ping_ok": true,
            "is_master": false
        }
        """
        When I run command on host "valkey1"
        """
            rdsync host add valkey2 --priority 200
        """
        Then command return code should be "0"
        And command output should match regexp
        """
            host has been added
        """
        When valkey on host "valkey1" is killed
        And zookeeper node "/test/last_switch" should match json within "60" seconds
        """
        {
            "cause": "auto",
            "from": "valkey1",
            "result": {
                "ok": true
            }
        }
        """
        Then valkey host "valkey2" should be master

    Scenario: Cluster mode failover works with dynamic quorum
        Given clustered shard is up and running
        Then zookeeper node "/test/health/valkey1" should match json within "30" seconds
        """
        {
            "ping_ok": true,
            "is_master": true,
            "is_read_only": false
        }
        """
        And zookeeper node "/test/health/valkey2" should match json within "30" seconds
        """
        {
            "ping_ok": true,
            "is_master": false
        }
        """
        And zookeeper node "/test/health/valkey3" should match json within "30" seconds
        """
        {
            "ping_ok": true,
            "is_master": false
        }
        """
        When host "valkey1" is stopped
        Then valkey host "valkey1" should become unavailable within "10" seconds
        And  zookeeper node "/test/manager" should match regexp within "30" seconds
        """
            .*valkey[23].*
        """
        And zookeeper node "/test/last_switch" should match json within "60" seconds
        """
        {
            "cause": "auto",
            "from": "valkey1",
            "result": {
                "ok": true
            }
        }
        """
        And zookeeper node "/test/active_nodes" should match json_exactly within "20" seconds
        """
        ["valkey2","valkey3"]
        """
        When I get zookeeper node "/test/master"
        And I save zookeeper query result as "new_master"
        Then valkey host "{{.new_master}}" should be master
        When I delete zookeeper node "/test/last_switch"
        When host "{{.new_master}}" is stopped
        Then valkey host "{{.new_master}}" should become unavailable within "10" seconds
        And zookeeper node "/test/last_switch" should match json within "60" seconds
        """
        {
            "cause": "auto",
            "result": {
                "ok": true
            }
        }
        """
        When I get zookeeper node "/test/master"
        And I save zookeeper query result as "new_master"
        Then valkey host "{{.new_master}}" should be master

    Scenario: Cluster mode failover cooldown is respected
        Given clustered shard is up and running
        Then zookeeper node "/test/health/valkey1" should match json within "30" seconds
        """
        {
            "ping_ok": true,
            "is_master": true,
            "is_read_only": false
        }
        """
        And zookeeper node "/test/health/valkey2" should match json within "30" seconds
        """
        {
            "ping_ok": true,
            "is_master": false
        }
        """
        And zookeeper node "/test/health/valkey3" should match json within "30" seconds
        """
        {
            "ping_ok": true,
            "is_master": false
        }
        """
        When host "valkey1" is stopped
        Then valkey host "valkey1" should become unavailable within "10" seconds
        And  zookeeper node "/test/manager" should match regexp within "30" seconds
        """
            .*valkey[23].*
        """
        And zookeeper node "/test/last_switch" should match json within "60" seconds
        """
        {
            "cause": "auto",
            "from": "valkey1",
            "result": {
                "ok": true
            }
        }
        """
        When I get zookeeper node "/test/master"
        And I save zookeeper query result as "new_master"
        Then valkey host "{{.new_master}}" should be master
        When host "valkey1" is started
        Then valkey host "valkey1" should become available within "20" seconds
        And valkey host "valkey1" should become replica of "{{.new_master}}" within "30" seconds
        When host "{{.new_master}}" is stopped
        Then valkey host "{{.new_master}}" should become unavailable within "10" seconds
        And valkey host "valkey1" should become replica of "{{.new_master}}" within "30" seconds
        And zookeeper node "/test/manager" should match regexp within "10" seconds
        """
            .*valkey.*
        """
        When I get zookeeper node "/test/manager"
        And I save zookeeper query result as "new_manager"
        And I wait for "60" seconds
        And I run command on host "{{.new_manager.hostname}}"
        """
            grep ERROR /var/log/rdsync.log
        """
        Then command output should match regexp
        """
            .*not enough time from last failover.*
        """

    Scenario: Cluster mode failover delay is respected
        Given clustered shard is up and running
        Then zookeeper node "/test/health/valkey1" should match json within "30" seconds
        """
        {
            "ping_ok": true,
            "is_master": true,
            "is_read_only": false
        }
        """
        And zookeeper node "/test/health/valkey2" should match json within "30" seconds
        """
        {
            "ping_ok": true,
            "is_master": false
        }
        """
        And zookeeper node "/test/health/valkey3" should match json within "30" seconds
        """
        {
            "ping_ok": true,
            "is_master": false
        }
        """
        When host "valkey1" is stopped
        Then valkey host "valkey1" should become unavailable within "10" seconds
        When I wait for "10" seconds
        Then valkey host "valkey2" should be replica of "valkey1"
        Then valkey host "valkey3" should be replica of "valkey1"
        When I get zookeeper node "/test/manager"
        And I save zookeeper query result as "new_manager"
        And I run command on host "{{.new_manager.hostname}}"
        """
            grep ERROR /var/log/rdsync.log
        """
        Then command output should match regexp
        """
            .*Failover was not approved.* .*failover timeout is not yet elapsed.*
        """

    Scenario: Cluster mode failover works for 2 node shard
        Given clustered shard is up and running
        When host "valkey3" is deleted
        Then valkey host "valkey3" should become unavailable within "10" seconds
        And zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["valkey1","valkey2"]
        """
        When host "valkey1" is stopped
        Then valkey host "valkey1" should become unavailable within "10" seconds
        And  zookeeper node "/test/manager" should match regexp within "30" seconds
        """
            .*valkey2.*
        """
        And zookeeper node "/test/last_switch" should match json within "60" seconds
        """
        {
            "cause": "auto",
            "from": "valkey1",
            "result": {
                "ok": true
            }
        }
        """
        Then valkey host "valkey2" should be master
        When host "valkey1" is started
        Then valkey host "valkey1" should become available within "20" seconds
        And valkey host "valkey1" should become replica of "valkey2" within "30" seconds

    Scenario: Cluster mode failover fails for 2 node shard with lagging replica
        Given clustered shard is up and running
        When host "valkey3" is deleted
        Then valkey host "valkey3" should become unavailable within "10" seconds
        And zookeeper node "/test/active_nodes" should match json_exactly within "30" seconds
        """
            ["valkey1","valkey2"]
        """
        When host "valkey2" is stopped
        Then valkey host "valkey2" should become unavailable within "10" seconds
        And zookeeper node "/test/active_nodes" should match json_exactly within "60" seconds
        """
            ["valkey1"]
        """
        When I wait for "30" seconds
        When I run command on valkey host "valkey1"
        """
            SET MYKEY TESTVALUE
        """
        Then valkey cmd result should match regexp
        """
            OK
        """
        When I wait for "30" seconds
        And host "valkey1" is stopped
        Then valkey host "valkey1" should become unavailable within "10" seconds
        When host "valkey2" is started
        Then valkey host "valkey2" should become available within "10" seconds
        Then zookeeper node "/test/manager" should match regexp within "10" seconds
        """
            .*valkey2.*
        """
        Then zookeeper node "/test/master" should match regexp
        """
            .*valkey1.*
        """
        When I wait for "60" seconds
        When I run command on host "valkey2"
        """
            grep Failover /var/log/rdsync.log
        """
        Then command output should match regexp
        """
            .*Failover was not approved.* .*no quorum.*
        """

    Scenario: Cluster mode master restart with disabled persistence causes failover
        Given clustered shard is up and running
        And persistence is disabled
        Then zookeeper node "/test/health/valkey1" should match json within "30" seconds
        """
        {
            "ping_ok": true,
            "is_master": true,
            "is_read_only": false
        }
        """
        And zookeeper node "/test/health/valkey2" should match json within "30" seconds
        """
        {
            "ping_ok": true,
            "is_master": false
        }
        """
        And zookeeper node "/test/health/valkey3" should match json within "30" seconds
        """
        {
            "ping_ok": true,
            "is_master": false
        }
        """
        When I run command on valkey host "valkey1"
        """
            SET very-important-key foo
        """
        And I wait for "1" seconds
        And valkey on host "valkey1" is restarted
        And zookeeper node "/test/last_switch" should match json within "60" seconds
        """
        {
            "cause": "auto",
            "from": "valkey1",
            "result": {
                "ok": true
            }
        }
        """
        When I get zookeeper node "/test/master"
        And I save zookeeper query result as "new_master"
        Then valkey host "{{.new_master}}" should be master
        And valkey host "valkey1" should become available within "20" seconds
        And valkey host "valkey1" should become replica of "{{.new_master}}" within "30" seconds
        When I run command on valkey host "{{.new_master}}"
        """
            GET very-important-key
        """
        Then valkey cmd result should match regexp
        """
            .*foo.*
        """
