# Golangによるcralwer

rubyで実装していたが、eventmachineによるコードは煩雑になり、メンテがしんどい。
rubyでの並列処理は仕様上の限界がある(ジャイアントロック)。
つまり、rubyは糞遅い。
ちょっとなんとかしたいなとずっとおもっていた。

## Goでの再実装について
goを習得しようとしている。
goroutineもあるので並列処理をかんたんに書けるのではないかと思っている
ちょうどよい題材かと思い、goで書き直すことにした。

## 速度
rubyだと15分かかっていた処理が5秒で終わってしまった。
すごすぎるぜGo言語

## Config

    {
        "downloaddir": "/Users/seijiro/Downloads/video",
        "dbfile": "/path/to/crawler.db",
        "title_regexp": ".*"
    }

downloaddir : dir file to download  
dbfile : path to dbfile  
title_regexp : fetch file only if title matches this regexp. 
