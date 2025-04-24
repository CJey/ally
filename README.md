# I am your ally!

**>= go1.24 required**

Ally是一个进程管理框架，在托管**服务自身具备优雅停机能力**的前提下，通过Ally可以实现优雅重启和优雅升级的能力。

## 构件

Ally有两个组成部分

* Ally管理程序: `make`
  * `make deb`, 生成适用于dpkg系统的deb安装包
  * `make rpm`, 生成适用于rpm系统的rpm安装包
* Ally go package: `import github.com/cjey/ally`

在服务中集成了Ally go package后，对于网络的Listen全都通过Ally go package代理调用，再通过Ally管理程序拉起服务，即可拥有优雅重启和优雅升级的能力。

## 原理

**一般状态**

1. Ally管理程序拉起服务
2. 托管服务启动后，调用Ally go package的Listen方法来获取Listener
3. Ally go package 检测到当前服务正在被Ally管理程序托管，向Ally管理程序发起rpc请求，申请服务要求的Listener
4. Ally管理程序接收到来自Ally go package的代理Listen请求后，执行Listen，并将得到Listener的fd通过unix socket回传给Ally go package
5. Ally go package 得到来自Ally管理程序的Listener fd后，反向包装为标准的Listener，并返回给托管服务
6. 至此，托管服务通过Ally得到了期望的Listener

**优雅升级**

1. 将新版本的服务binary替换掉原始binary后(不做此操作，即等于**优雅重启**)，执行 ally reload {appname}
2. Ally管理服务程序收到reload信号后，以文件系统上当前最新版本的binary拉起新的服务（此时新老版本共存）
3. 新版本服务正常向Ally go package请求Listen
4. Ally管理程序将此前已经得到的Listener的fd传递给新版本服务
5. 新版本服务得到的Listener即是老版本服务的Listener(复用)
6. 新版本服务正常执行Serving后，调用Ally go package的Ready()，向Ally管理程序通报自身已经准备就绪
7. Ally管理程序检测到新版本服务准备就绪后，向其他版本的存活进行发起优雅停机信号
8. 旧版本服务接收到优雅停机信号后，应当立即关闭掉Listener，同时继续妥善处理好当前活跃中的会话，并在会话处理完成后，无痛的终止活跃连接
9. 旧版本服务在所有会话结束，所有活跃连接关闭后，自行退出
10. 至此优雅升级完成

**优雅停机**

多数的官方库都具备此能力，如果是私有网络协议，则需要在实现上支持和具备此种能力

1. [http.Server.Shutdown](https://pkg.go.dev/net/http#Server.Shutdown)
2. [grpc.Server.GracefulStop](https://pkg.go.dev/google.golang.org/grpc#Server.GracefulStop)

## 优点

1. 与经典的Exec(fds...)拉起新进程的方式相比
    1. 同样是作为"Master"，ally不需要启动时就关心服务到底要Listen什么
    2. 服务可以先启动，随后根据需要动态的Listen，应对从配置中心动态获取配置的场景
    3. 服务自身不用关心复杂的fd传递过程，以及fd会被递归继承等副作用的应对
2. 编程模式上
    1. Ally提供了非常简单，兼容性极高的API，编程上和直接使用net.Listen方式几乎无区别，集成Ally很简单
    2. Ally go package支持simulate模式，当程序没有通过Ally管理程序启动时，除了丧失优雅升级能力外，无任何其他影响
3. 无状态服务完美支持
4. 有状态服务有限支持
    1. Ally框架在优雅升级服务期间，总是会存在一个新旧版本共存的区间，如果服务的长链接、长耗时任务较多，那么旧版本的进程可能会持续很久才会终止退出
    2. 如果旧版本中保留有状态数据，那么新的请求在新的版本服务中将无法获取
    3. 对于单例服务，还可能会碰到需要独占运行环境资源的需求，新旧版本的共存期间将会造成冲突，如对sqlite等嵌入式db的访问等
    4. Ally框架提供了3种状态协调能力用于有限支持上述场景，允许服务通过Ally go package将状态保存在Ally管理程序中，实现状态的保存、共享、协调
        1. Cache:  简单的内存kv cache
        2. Atomic: 原子数值访问
        3. Locker: 简单的局部分布式锁

## 特别注意

以grpc server为例，grpc server自身提供了优雅停机的能力，但当并发创建的新tcp连接数量较多时，通过ally reload时可能还是会出现一些底层的网络错误，如"connection closed before server preface received"。

大致的成因主要在于grpc server自身的优雅停机方式实现上，停机时，grpc对idle的grpc connection执行close操作，然而，grpc connection是一个高层的连接抽象，再下一层是http connection，再下一层是tcp connection。

网络的连接通常是从tcp connection开始，随后逐层wrap、逐层抽象后才会被grpc拿到，那么，假如在reload发生时，恰好有一条新的tcp connection刚创建完成，我们又要求grpc server执行优雅停机，会发生什么呢？

目前从结果上来看，grpc server会直接视此tcp connection为idle grpc connection，执行关闭操作。

可是，对grpc client而言，这是一条刚established tcp connection，尚在执行http协议、grpc协议的握手协商工作，server突然close了此连接，client会视此为错误连接并返回错误。

为了避免此问题，需要旧实例在接收到停机信号后，先主动Close Listener，随后等待一小段时间（建议是对应场景下最大rtt的3倍以上，最小50ms），保证Listener Close前接收到的新连接都可以完成基本的协议协商过程。但同时会带来一个副作用，Listener是在外部被Close的，因此会导致grpc server的serve过程出错，不过对此错误做出区别判定处理就可以解决。

### 设计说明

此问题其实可以由ally封装实现一个私有的net.Listener来屏蔽此问题：收到停机信号后立即close底层的tcp listener，但自身不做close，仍交给业务执行，这样业务也不会再得到新的connection，ally随后等待一段时间T，再通知业务停机。

但这在实现上存在一些顾虑
1. 增加了ally的复杂度
2. 中间多封装了一层，但ally更希望可以提供的是标准库的Listener和Connection类型，这样对于业务的适配难度和兼容性会更好
3. 这个方案能应对grpc，但能应对其他公开的、私有的协议吗？
4. 这个时间T究竟要多少合适？
   1. 引入默认值可能会造成服务停机时出现"意外"的等待时间，让开发者产生困惑
   2. 引入配置值，仍然会让开发者注意到此问题，并阅读文档理解此机制要解决的问题(如果不阅读完整的文档可能都无法理解此配置的设计意图)

综上，不如让开发者能主动了解此问题，并自行选择处理方式，同时还能保持ally自身的简单性。
