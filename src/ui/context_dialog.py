from PyQt6.QtWidgets import (
    QDialog, QVBoxLayout, QHBoxLayout, QLabel,
    QLineEdit, QPushButton, QFormLayout
)

class ContextDialog(QDialog):
    def __init__(self, parent=None):
        super().__init__(parent)
        self.setWindowTitle("添加上下文")
        self.setMinimumWidth(600)  # 从 300 改为 600
        self.setMinimumHeight(200) # 添加最小高度设置
        
        layout = QVBoxLayout(self)
        form_layout = QFormLayout()
        
        # 创建输入框
        self.name_input = QLineEdit()
        self.name_input.setMinimumWidth(400)
        self.name_input.setMinimumHeight(30)
        
        self.host_input = QLineEdit()
        self.host_input.setMinimumWidth(400)
        self.host_input.setMinimumHeight(30)
        
        self.port_input = QLineEdit()
        self.port_input.setMinimumWidth(400)
        self.port_input.setMinimumHeight(30)
        self.port_input.setText("11211")
        
        # 添加表单项
        form_layout.addRow("名称:", self.name_input)
        form_layout.addRow("主机:", self.host_input)
        form_layout.addRow("端口:", self.port_input)
        
        # 按钮布局
        button_layout = QHBoxLayout()
        self.ok_button = QPushButton("确定")
        self.cancel_button = QPushButton("取消")
        
        button_layout.addWidget(self.ok_button)
        button_layout.addWidget(self.cancel_button)
        
        # 连接信号
        self.ok_button.clicked.connect(self.accept)
        self.cancel_button.clicked.connect(self.reject)
        
        # 添加所有布局
        layout.addLayout(form_layout)
        layout.addLayout(button_layout)
    
    def get_context_data(self):
        return {
            "name": self.name_input.text(),
            "host": self.host_input.text(),
            "port": self.port_input.text()
        }