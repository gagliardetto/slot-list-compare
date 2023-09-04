# slot-list-compare

## Description

This is a simple tool to compare two lists of slots. It can be used to compare the slots from faithful and a solana RPC.

## Usage

```bash
$ slot-list-compare --rpc=https://api.mainnet-beta.solana.com --epoch 490 --faithful 490.slots.txt
``````

It will fetch the slot list for epoch 490 from the RPC and compare it to the slot list from faithful. It will print the slots that are missing in the RPC and the slots that are missing in faithful.
