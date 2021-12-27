import sys
import json
from web3 import Web3, HTTPProvider


def readacc():
    with open("accounts.json") as f:
        acclist = json.load(f)
        return acclist

def main():
    acclist = readacc()
    key = acclist[0]["key"]
    sender = acclist[0]["address"]
    receiver = acclist[1]["address"]

    url = "http://127.0.0.1:8545"

    node = Web3(HTTPProvider(url))

    tx={}
    tx['to'] = receiver
    tx['chainId'] = 2020
    tx['nonce'] = 0
    tx['gas'] = 90000
    tx['gasPrice'] = 1000000000
    tx['value'] = node.toWei(0.00000000000001,"ether")
    nonce = node.eth.getTransactionCount(sender, 'pending')
    tx['nonce'] = nonce
      
    signed = node.eth.account.signTransaction(tx, key)
    node.eth.sendRawTransaction(signed.rawTransaction)


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        sys.exit(0)

