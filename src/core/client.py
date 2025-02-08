from pymemcache.client.base import Client

class MemcachedClient:
    def __init__(self):
        self.client = None
    
    def connect(self, host='localhost', port=11211):
        try:
            self.client = Client((host, port))
            # 测试连接
            self.client.stats()
            return True
        except Exception as e:
            return False, str(e)
    
    def disconnect(self):
        if self.client:
            self.client.close()
            self.client = None