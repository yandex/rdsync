package jepsen.rdsync.waitquorum;

import redis.clients.jedis.Jedis;

public class WaitQuorumJedis extends Jedis {
    public WaitQuorumJedis(String url) {
        super(url);
    }

    public long waitQuorum() {
        checkIsInMultiOrPipeline();
        connection.sendCommand(WaitQuorumCommand.WAITQUORUM);
        return connection.getIntegerReply();
    }
}
