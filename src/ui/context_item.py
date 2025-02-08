from PyQt6.QtWidgets import QStyledItemDelegate, QStyle
from PyQt6.QtCore import Qt, QRect, QSize, QRectF, QPointF
from PyQt6.QtGui import QColor, QLinearGradient, QPainter, QFont, QPainterPath
import random

class ContextItemDelegate(QStyledItemDelegate):
    def __init__(self, parent=None):
        super().__init__(parent)
        self.colors = [
            ((240, 248, 255), (230, 230, 250)),  # Alice Blue to Lavender
            ((255, 240, 245), (255, 228, 225)),  # Lavender Blush to Misty Rose
            ((240, 255, 240), (245, 255, 250)),  # Honeydew to Mint Cream
            ((255, 250, 240), (255, 245, 238)),  # Floral White to Seashell
            ((240, 255, 255), (240, 248, 255)),  # Azure to Alice Blue
        ]
        self.item_colors = {}  # 存储每个项目的固定颜色

    def paint(self, painter, option, index):
        painter.save()
        
        # 获取或创建项目的固定颜色
        item_id = id(index.data())
        if item_id not in self.item_colors:
            self.item_colors[item_id] = random.choice(self.colors)
        color_pair = self.item_colors[item_id]
        
        # 绘制背景
        rect = option.rect
        
        # 创建渐变
        gradient = QLinearGradient(
            QPointF(rect.topLeft()),
            QPointF(rect.bottomRight())
        )
        
        # 设置随机渐变背景
        color_pair = random.choice(self.colors)
        gradient.setColorAt(0, QColor(*color_pair[0]))
        gradient.setColorAt(1, QColor(*color_pair[1]))
        
        # 填充背景
        painter.fillRect(rect, gradient)
        
        # 获取数据
        data = index.data()
        name = data.split(" (")[0]
        host_port = data.split(" (")[1].rstrip(")")
        
        # 绘制内容
        content_rect = QRect(rect)
        content_rect.adjust(10, 10, -10, -10)
        
        # 绘制名称
        name_font = QFont()
        name_font.setPointSize(12)
        name_font.setBold(True)
        painter.setFont(name_font)
        
        name_rect = QRect(content_rect)
        name_rect.setHeight(30)
        painter.drawText(name_rect, Qt.AlignmentFlag.AlignCenter, name)
        
        # 绘制服务器信息
        info_font = QFont()
        info_font.setPointSize(10)
        painter.setFont(info_font)
        
        info_rect = QRect(content_rect)
        info_rect.setTop(name_rect.bottom() + 5)
        painter.drawText(info_rect, Qt.AlignmentFlag.AlignHCenter, f"Server: {host_port}")
        
        # 绘制服务器数量
        count_rect = QRect(content_rect)
        count_rect.setTop(info_rect.top() + 20)
        painter.drawText(count_rect, Qt.AlignmentFlag.AlignHCenter, "Servers: 1")
        
        # 如果被选中，添加选中效果
        if option.state & QStyle.StateFlag.State_Selected:
            painter.fillRect(rect, QColor(51, 153, 255, 50))
            
        # 绘制底部分割线
        painter.setPen(QColor(200, 200, 200))
        painter.drawLine(rect.left(), rect.bottom(), rect.right(), rect.bottom())
        
        painter.restore()

    def sizeHint(self, option, index):
        return QSize(option.rect.width(), 100)