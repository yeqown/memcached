import os
import json
import random

from PyQt6.QtWidgets import (
    QMainWindow, QWidget, QVBoxLayout, QHBoxLayout,
    QPushButton, QLineEdit, QLabel, QStatusBar,
    QListWidget, QSplitter, QTextEdit, QGroupBox,
    QMessageBox, QListWidgetItem
)
from PyQt6.QtCore import Qt

from core.client import MemcachedClient
from ui.context_dialog import ContextDialog
from ui.context_item import ContextItemDelegate

class MainWindow(QMainWindow):
    def __init__(self):
        super().__init__()
        self.setWindowTitle("Memcached GUI")
        self.setMinimumSize(1200, 800)
        
        # 添加上下文配置文件路径
        self.config_file = "contexts.json"
        
        # 创建中心部件
        central_widget = QWidget()
        self.setCentralWidget(central_widget)
        
        # 创建主布局
        main_layout = QHBoxLayout(central_widget)
        
        # 创建主分割器
        main_splitter = QSplitter(Qt.Orientation.Horizontal)
        
        # 左侧上下文管理区域
        context_group = QGroupBox("Contexts")
        context_layout = QVBoxLayout()
        
        # 上下文列表
        self.context_list = QListWidget()
        self.context_list.setSpacing(0)  # 移除间距
        self.context_list.setViewMode(QListWidget.ViewMode.ListMode)  # 改为列表模式
        self.context_list.setMovement(QListWidget.Movement.Static)
        self.context_list.setItemDelegate(ContextItemDelegate(self.context_list, self.handle_delete_context, self.handle_add_context))
        self.context_list.setStyleSheet("""
            QListWidget {
                border: none;
            }
            QListWidget::item {
                padding: 0px;
                border: none;
            }
            QListWidget::item:selected {
                background: rgba(0, 0, 0, 10);
                border: 1px solid #ccc;
            }
        """)
        
        # # 删除重复的列表配置代码
        # self.context_list.setSpacing(10)  # 设置项目间距
        # self.context_list.setViewMode(QListWidget.ViewMode.IconMode)
        # self.context_list.setMovement(QListWidget.Movement.Static)
        # self.context_list.setResizeMode(QListWidget.ResizeMode.Adjust)
        # self.context_list.setItemDelegate(ContextItemDelegate(self.context_list))
        # self.context_list.setStyleSheet("""
        #     QListWidget {
        #         background: white;
        #         padding: 10px;
        #     }
        #     QListWidget::item {
        #         background: transparent;
        #         border: none;
        #     }
        #     QListWidget::item:selected {
        #         background: rgba(0, 0, 0, 10);
        #         border: 1px solid #ccc;
        #     }
        # """)
        
        # 上下文管理按钮
        # ... 前面的代码保持不变 ...
        
        # 移除原有的按钮布局
        # context_buttons_layout = QHBoxLayout()
        # self.add_context_btn = QPushButton("Add")
        # self.delete_context_btn = QPushButton("Delete")
        # context_buttons_layout.addWidget(self.add_context_btn)
        # context_buttons_layout.addWidget(self.delete_context_btn)
        
        context_layout.addWidget(self.context_list)
        # context_layout.addLayout(context_buttons_layout)  # 移除这行
        context_group.setLayout(context_layout)
        
        # 右侧操作和展示区域
        right_widget = QWidget()
        right_layout = QVBoxLayout(right_widget)
        right_layout.setStretch(0, 2)  # 操作区域占 20%
        right_layout.setStretch(1, 8)  # 展示区域占 80%
        
        # 操作区域
        operation_group = QGroupBox("Operations")
        operation_layout = QHBoxLayout()
        
        self.connect_btn = QPushButton("Connect")
        self.key_input = QLineEdit()
        self.key_input.setPlaceholderText("Enter key")
        
        operation_layout.addWidget(self.connect_btn)
        operation_layout.addWidget(self.key_input)
        operation_group.setLayout(operation_layout)
        
        # 展示区域分割器
        display_splitter = QSplitter(Qt.Orientation.Horizontal)
        
        # 左侧协议交互区域
        self.protocol_text = QTextEdit()
        self.protocol_text.setReadOnly(True)
        self.protocol_text.setPlaceholderText("Protocol Interaction")
        
        # 右侧键值信息区域
        self.value_text = QTextEdit()
        self.value_text.setReadOnly(True)
        self.value_text.setPlaceholderText("Key-Value Information")
        
        display_splitter.addWidget(self.protocol_text)
        display_splitter.addWidget(self.value_text)
        display_splitter.setSizes([400, 400])  # 设置初始宽度比例
        
        # 添加所有组件到右侧布局
        right_layout.addWidget(operation_group)
        right_layout.addWidget(display_splitter)
        right_layout.setStretchFactor(operation_group, 2)
        right_layout.setStretchFactor(display_splitter, 8)
        
        # 将左右两个主要部分添加到主分割器
        main_splitter.addWidget(context_group)
        main_splitter.addWidget(right_widget)
        main_splitter.setSizes([300, 900])  # 设置初始宽度比例
        
        # 添加主分割器到主布局
        main_layout.addWidget(main_splitter)
        
        # 添加状态栏
        self.status_bar = QStatusBar()
        self.setStatusBar(self.status_bar)
        
        # 连接信号
        self.connect_btn.clicked.connect(self.handle_connect)
        # self.add_context_btn.clicked.connect(self.handle_add_context)      # Remove this line
        # self.delete_context_btn.clicked.connect(self.handle_delete_context)  # Remove this line
        
        # 添加 Memcached 客户端
        self.memcached_client = MemcachedClient()
        self.is_connected = False
        
        # 修改连接按钮的初始状态
        self.connect_btn.setEnabled(False)
        
        # 添加上下文选择信号
        self.context_list.currentItemChanged.connect(self.handle_context_changed)
        
        # 加载已保存的上下文
        self.load_contexts()
    
    def handle_context_changed(self, current, previous):
        """处理上下文选择变化"""
        self.connect_btn.setEnabled(current is not None)
        if self.is_connected:
            self.disconnect_memcached()
    
    def get_current_context(self):
        """获取当前选中的上下文配置"""
        current_item = self.context_list.currentItem()
        if not current_item:
            return None
            
        item_text = current_item.text()
        name = item_text.split(" (")[0]
        host_port = item_text.split(" (")[1].rstrip(")")
        host, port = host_port.split(":")
        return {
            "name": name,
            "host": host,
            "port": port
        }
    
    def disconnect_memcached(self):
        """断开 Memcached 连接"""
        self.memcached_client.disconnect()
        self.is_connected = False
        self.connect_btn.setText("Connect")
        self.key_input.setEnabled(False)
        self.status_bar.showMessage("已断开连接")
        self.protocol_text.append("已断开连接")
    
    def handle_connect(self):
        """处理连接/断开操作"""
        if self.is_connected:
            self.disconnect_memcached()
            return
            
        context = self.get_current_context()
        if not context:
            QMessageBox.warning(self, "错误", "请先选择一个上下文")
            return
            
        try:
            result = self.memcached_client.connect(context["host"], int(context["port"]))
            if isinstance(result, tuple):
                success, error = result
                if not success:
                    raise Exception(error)
                    
            self.is_connected = True
            self.connect_btn.setText("Disconnect")
            self.key_input.setEnabled(True)
            self.status_bar.showMessage(f"已连接到 {context['host']}:{context['port']}")
            self.protocol_text.append(f"已连接到 {context['host']}:{context['port']}")
            
        except Exception as e:
            QMessageBox.critical(self, "连接错误", str(e))
            self.status_bar.showMessage(f"连接失败: {str(e)}")
            self.protocol_text.append(f"连接失败: {str(e)}")
    
    def save_contexts(self):
        """保存上下文配置"""
        contexts = []
        colors = [
            ((240, 248, 255), (230, 230, 250)),  # Alice Blue to Lavender
            ((255, 240, 245), (255, 228, 225)),  # Lavender Blush to Misty Rose
            ((240, 255, 240), (245, 255, 250)),  # Honeydew to Mint Cream
            ((255, 250, 240), (255, 245, 238)),  # Floral White to Seashell
            ((240, 255, 255), (240, 248, 255)),  # Azure to Alice Blue
        ]
        
        for i in range(self.context_list.count()):
            item = self.context_list.item(i)
            item_text = item.text()
            
            # Skip the "+" item
            if item_text == "+":
                continue
                
            name = item_text.split(" (")[0]
            host_port = item_text.split(" (")[1].rstrip(")")
            host, port = host_port.split(":")
            
            # 为每个 context 添加随机颜色
            context = {
                "name": name,
                "host": host,
                "port": port,
                "colors": random.choice(colors)  # 添加随机颜色
            }
            contexts.append(context)
        
        try:
            with open(self.config_file, 'w') as f:
                json.dump(contexts, f)
        except Exception as e:
            self.status_bar.showMessage(f"保存配置失败: {str(e)}")

    def load_contexts(self):
        """加载保存的上下文配置"""
        if os.path.exists(self.config_file):
            try:
                with open(self.config_file, 'r') as f:
                    contexts = json.load(f)
                    for context in contexts:
                        item = QListWidgetItem(f"{context['name']} ({context['host']}:{context['port']})")
                        item.setData(Qt.ItemDataRole.UserRole, context.get('colors'))
                        self.context_list.addItem(item)
            except Exception as e:
                self.status_bar.showMessage(f"加载配置失败: {str(e)}")
        
        # 添加"添加"按钮项
        self.context_list.addItem("+")
    
    def handle_add_context(self):
        """处理添加上下文"""
        dialog = ContextDialog(self)
        if dialog.exec():
            context_data = dialog.get_context_data()
            if not context_data["name"] or not context_data["host"]:
                QMessageBox.warning(self, "错误", "名称和主机不能为空")
                return
            
            # 添加到列表
            self.context_list.addItem(
                f"{context_data['name']} ({context_data['host']}:{context_data['port']})"
            )
            # 保存配置
            self.save_contexts()
            self.status_bar.showMessage("上下文添加成功")
    
    def handle_delete_context(self, index):
        """处理删除上下文"""
        row = index.row()

        if self.context_list.item(row).text() == "+":
            return
        
        reply = QMessageBox.question(
            self, "确认删除",
            "确定要删除选中的上下文吗？",
            QMessageBox.StandardButton.Yes | QMessageBox.StandardButton.No
        )
        
        if reply == QMessageBox.StandardButton.Yes:
            self.context_list.takeItem(row)
            self.save_contexts()
            self.status_bar.showMessage("上下文删除成功")